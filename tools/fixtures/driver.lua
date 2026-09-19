-- The Go generator runs this script in a private working directory.
local function read_bytes(path)
  local file = assert(io.open(path, 'rb'))
  local data = file:read('*a')
  assert(file:close())
  return data
end

local function write_bytes(path, data)
  local file = assert(io.open(path, 'wb'))
  assert(file:write(data))
  assert(file:close())
end

local function hex(text)
  return (text:gsub('.', function(char)
    return string.format('%02x', string.byte(char))
  end))
end

local function unhex(text)
  return (text:gsub('%x%x', function(byte)
    return string.char(tonumber(byte, 16))
  end))
end

local function buffer_state()
  local lines = vim.api.nvim_buf_get_lines(0, 0, -1, true)
  for i, line in ipairs(lines) do
    lines[i] = hex(line)
  end

  return {
    seq = vim.fn.undotree().seq_cur,
    lines_hex = lines,
    endofline = vim.bo.endofline,
    fileformat = vim.bo.fileformat,
    fileencoding = vim.bo.fileencoding,
  }
end

local function check_abi()
  local ffi = require('ffi')

  -- Layout from the pinned extmark.h and pos_defs.h.
  ffi.cdef [[
    typedef struct {
      int start_row;
      int start_col;
      int old_row;
      int old_col;
      int new_row;
      int new_col;
      ptrdiff_t start_byte;
      ptrdiff_t old_byte;
      ptrdiff_t new_byte;
    } XunhenExtmarkLayout;
  ]]

  assert(ffi.os == 'Linux')
  assert(ffi.arch == 'x64' and ffi.abi('le'))
  assert(ffi.sizeof('int') == 4)
  assert(ffi.sizeof('ptrdiff_t') == 8)
  assert(ffi.sizeof('XunhenExtmarkLayout') == 48)
  assert(ffi.offsetof('XunhenExtmarkLayout', 'start_byte') == 24)
end

local function configure(recipe)
  vim.o.modeline = false
  vim.o.exrc = false
  vim.o.swapfile = false
  vim.o.backup = false
  vim.o.writebackup = false
  vim.o.shadafile = 'NONE'

  -- Absolute TMPDIR paths may contain commas, which undodir treats as separators.
  vim.o.undodir = './undo'
  vim.o.undofile = recipe.persistence == 'automatic'
  vim.o.undolevels = recipe.undo_levels

  vim.o.fixeol = false
  vim.o.fileencodings = ''
  vim.o.fileformats = recipe.fileformat
end

local function open_source(recipe)
  local command = string.format(
    'silent edit ++enc=%s ++bad=keep ++ff=%s source.bin',
    recipe.encoding,
    recipe.fileformat
  )

  vim.cmd(command)
  vim.bo.fixeol = false
end

local function sync(recipe)
  -- Neovim's tests use this to split sourced edits into separate undo blocks.
  vim.cmd('setlocal undolevels=' .. recipe.undo_levels)
end

local function apply_step(recipe, step)
  if step.op == 'edit' then
    local lines = {}
    for i, encoded in ipairs(step.lines_hex or {}) do
      lines[i] = unhex(encoded)
    end

    vim.api.nvim_buf_set_lines(0, step.start or 0, step['end'] or 0, true, lines)
    sync(recipe)

  elseif step.op == 'undo' then
    vim.cmd('silent undo ' .. (step.seq or 0))

  elseif step.op == 'join' then
    vim.cmd('undojoin')

  elseif step.op == 'save' then
    vim.cmd('silent write')

  elseif step.op == 'reopen' then
    local before = vim.fn.undotree().seq_cur
    vim.cmd('bwipeout!')
    open_source(recipe)
    assert(vim.fn.undotree().seq_cur == before, 'automatic undo reload failed')

  elseif step.op == 'endofline' then
    vim.bo.endofline = step.value or false

  elseif step.op == 'move' then
    vim.cmd('silent 1move $')
    sync(recipe)

  else
    error('unknown fixture operation')
  end
end

local function undo_path()
  return vim.fn.undofile(vim.fn.fnamemodify('source.bin', ':p'))
end

local function create_history(recipe)
  local observations = {
    { op = 'initial', state = buffer_state() },
  }

  for _, step in ipairs(recipe.steps) do
    apply_step(recipe, step)
    observations[#observations + 1] = {
      op = step.op,
      state = buffer_state(),
    }
  end

  if recipe.persistence == 'automatic' then
    vim.cmd('silent write')
    write_bytes('history.undo', read_bytes(undo_path()))
  else
    vim.cmd('silent wundo history.undo')
  end

  local result = {
    anchor = buffer_state(),
    tree = vim.fn.undotree(),
    observations = observations,
    saved_hex = hex(read_bytes('source.bin')),
  }
  write_bytes('created.json', vim.json.encode(result))
end

local function check_pruned_states(sequences)
  for _, seq in ipairs(sequences) do
    local before = buffer_state()
    local ok, err = pcall(vim.cmd, 'silent undo ' .. seq)

    assert(not ok, 'pruned state unexpectedly recoverable')
    assert(tostring(err):find('E830', 1, true), 'unexpected undo error')
    assert(vim.deep_equal(before, buffer_state()), 'failed selection changed text')
  end
end

local function reject_wrong_base()
  vim.cmd('bwipeout!')
  write_bytes('mismatch.bin', 'deliberately different base\n')
  vim.cmd('silent edit mismatch.bin')

  local result = vim.api.nvim_exec2('rundo history.undo', { output = true })
  assert(#vim.fn.undotree().entries == 0, 'mismatched base unexpectedly accepted')
  return result.output
end

local function read_history(recipe)
  vim.bo.undofile = false
  if recipe.persistence == 'explicit' then
    vim.cmd('silent rundo history.undo')
  end

  local tree = vim.fn.undotree()
  local anchor = buffer_state()
  assert(#tree.entries > 0, 'reference undo history did not load')

  local states = {}
  for _, seq in ipairs(recipe.visit) do
    vim.cmd('silent undo ' .. seq)
    local state = buffer_state()
    assert(state.seq == seq, 'undo selected another state')
    states[#states + 1] = state
  end

  check_pruned_states(recipe.pruned)
  local mismatch_message = reject_wrong_base()

  write_bytes('loaded.json', vim.json.encode({
    anchor = anchor,
    tree = tree,
    states = states,
    mismatch_rejected = true,
    mismatch_message = mismatch_message,
  }))
end

local function main()
  local recipe = vim.json.decode(read_bytes('request.json'))
  check_abi()
  configure(recipe)

  if recipe.mode == 'create' then
    open_source(recipe)
    create_history(recipe)
    return
  end

  local loaded_path = 'history.undo'
  if recipe.persistence == 'automatic' then
    -- Automatic loading uses a different hash for empty files than :rundo.
    loaded_path = undo_path()
    write_bytes(loaded_path, read_bytes('history.undo'))
  end
  local undo_before = read_bytes(loaded_path)

  open_source(recipe)
  read_history(recipe)
  assert(read_bytes(loaded_path) == undo_before, 'replay changed the active undo file')
end

local ok, err = xpcall(main, debug.traceback)
if not ok then
  io.stderr:write(tostring(err), '\n')
  vim.cmd('cquit 1')
end
vim.cmd('qa!')
