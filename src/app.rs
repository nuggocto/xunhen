use crate::{
    Error,
    budget::{Budget, Reservation},
    diff::{Diff, Kind},
    git::{self, Cancellation},
    output,
    repository::Repository,
    signals::Signals,
    terminal::{self, TerminalGuard},
};
use crossterm::event::{self, Event, KeyCode, KeyEventKind, KeyModifiers};
use ratatui::{
    Frame,
    layout::Rect,
    style::{Color, Modifier, Style},
    text::{Line, Span},
    widgets::{Block, Borders, List, ListItem, ListState, Paragraph},
};
use std::{
    path::PathBuf,
    sync::mpsc::{self, Receiver, SyncSender, TrySendError},
    thread,
    time::Duration,
};

struct FileLabel {
    path: String,
    unavailable: bool,
}
enum ResultData {
    Loaded(Vec<FileLabel>, Reservation),
    Patch(Diff),
}
enum Request {
    Load,
    Patch(usize),
}
struct Work {
    generation: u64,
    cancel: Cancellation,
    request: Request,
}
type Reply = (u64, Result<ResultData, Error>);

struct Worker {
    send: Option<SyncSender<Work>>,
    receive: Receiver<Reply>,
    handle: Option<thread::JoinHandle<()>>,
    cancel: Cancellation,
}

impl Worker {
    fn new(git: PathBuf, directory: PathBuf) -> Result<Self, Error> {
        let (send, requests) = mpsc::sync_channel::<Work>(1);
        let (replies, receive) = mpsc::sync_channel::<Reply>(1);
        let handle = thread::Builder::new()
            .name("repository".into())
            .spawn(move || {
                let runtime = tokio::runtime::Builder::new_current_thread()
                    .enable_all()
                    .max_blocking_threads(1)
                    .build();
                let budget = Budget::new();
                let mut repository: Option<Repository> = None;
                while let Ok(work) = requests.recv() {
                    if work.cancel.check().is_err() {
                        continue;
                    }
                    let result = match &runtime {
                        Err(_) => Err(Error::Unavailable("repository runtime could not start")),
                        Ok(runtime) => runtime.block_on(async {
                            match work.request {
                                Request::Load => {
                                    repository = None;
                                    let loaded = Repository::load(
                                        git.clone(),
                                        directory.clone(),
                                        budget.clone(),
                                        work.cancel.clone(),
                                    )
                                    .await?;
                                    let reservation = budget.reserve(8 * 1024 * 1024)?;
                                    let mut labels = Vec::with_capacity(loaded.files.len());
                                    let mut bytes =
                                        labels.capacity() * std::mem::size_of::<FileLabel>();
                                    for file in &loaded.files {
                                        if bytes + 2 * crate::limits::DISPLAY_BYTES
                                            > 8 * 1024 * 1024
                                        {
                                            return Err(Error::Budget("file list"));
                                        }
                                        let label = output::path(&file.path);
                                        bytes += label.capacity();
                                        labels.push(FileLabel {
                                            path: label,
                                            unavailable: file.reason.is_some(),
                                        });
                                    }
                                    repository = Some(loaded);
                                    Ok(ResultData::Loaded(labels, reservation))
                                }
                                Request::Patch(index) => {
                                    let repository = repository.as_mut().ok_or(Error::Stale)?;
                                    repository.runner.cancel = work.cancel.clone();
                                    let diff = repository.patch(index).await?;
                                    if diff.retained_bytes() > 8 * 1024 * 1024 {
                                        return Err(Error::Budget("comparison memory"));
                                    }
                                    Ok(ResultData::Patch(diff))
                                }
                            }
                        }),
                    };
                    let mut reply = (work.generation, result);
                    loop {
                        match replies.try_send(reply) {
                            Ok(()) | Err(TrySendError::Disconnected(_)) => break,
                            Err(TrySendError::Full(value)) => reply = value,
                        }
                        if work.cancel.check().is_err() {
                            break;
                        }
                        thread::sleep(Duration::from_millis(5));
                    }
                }
            })
            .map_err(|e| Error::io("start repository worker", e))?;
        Ok(Self {
            send: Some(send),
            receive,
            handle: Some(handle),
            cancel: Cancellation::default(),
        })
    }
}

impl Worker {
    fn shutdown(&mut self) -> Result<(), Error> {
        self.cancel.cancel();
        self.send.take();
        // Cancellation also releases a worker waiting to publish its last result.
        if let Some(handle) = self.handle.take() {
            let deadline = std::time::Instant::now() + Duration::from_secs(3);
            while !handle.is_finished() {
                if std::time::Instant::now() >= deadline {
                    return Err(Error::Unavailable(
                        "worker shutdown timed out; abandoned snapshots will be recovered",
                    ));
                }
                thread::sleep(Duration::from_millis(5));
            }
            handle
                .join()
                .map_err(|_| Error::Unavailable("repository worker panicked"))?;
        }
        Ok(())
    }
}

impl Drop for Worker {
    fn drop(&mut self) {
        let _ = self.shutdown();
    }
}

struct App {
    files: Vec<FileLabel>,
    _labels: Option<Reservation>,
    selected: usize,
    file_offset: usize,
    files_focus: bool,
    diff: Option<Diff>,
    row: usize,
    column: u16,
    message: String,
    generation: u64,
    pending: Option<Work>,
    loading: bool,
}

impl App {
    fn request(&mut self, worker: &mut Worker, request: Request) -> Result<(), Error> {
        worker.cancel.cancel();
        worker.cancel = Cancellation::default();
        self.generation = self
            .generation
            .checked_add(1)
            .ok_or(Error::Budget("operation generation"))?;
        self.pending = Some(Work {
            generation: self.generation,
            cancel: worker.cancel.clone(),
            request,
        });
        self.diff = None;
        self.row = 0;
        self.column = 0;
        self.loading = true;
        self.message = "Loading... Esc cancels".into();
        Ok(())
    }

    fn draw(&mut self, frame: &mut Frame) {
        let area = frame.area();
        if area.width == 0 || area.height == 0 {
            return;
        }
        if area.width < 12 || area.height < 4 {
            frame.render_widget(Paragraph::new("q quit"), area);
            return;
        }
        let body = Rect::new(area.x, area.y, area.width, area.height.saturating_sub(3));
        let status = Rect::new(area.x, body.bottom(), area.width, 3);
        let wide = area.width >= 90;
        let file_width = if wide {
            (area.width / 3).min(40)
        } else {
            area.width
        };
        if wide || self.files_focus {
            let pane = Rect::new(body.x, body.y, file_width, body.height);
            let visible = usize::from(pane.height.saturating_sub(2)).max(1);
            self.file_offset = self
                .file_offset
                .min(self.files.len().saturating_sub(visible));
            if self.selected < self.file_offset {
                self.file_offset = self.selected;
            } else if self.selected >= self.file_offset + visible {
                self.file_offset = self.selected + 1 - visible;
            }
            let items: Vec<_> = self
                .files
                .iter()
                .skip(self.file_offset)
                .take(visible)
                .map(|file| {
                    ListItem::new(format!(
                        "{}{}",
                        if file.unavailable { "! " } else { "" },
                        file.path
                    ))
                })
                .collect();
            let mut state = ListState::default().with_selected(
                self.files
                    .get(self.selected)
                    .map(|_| self.selected - self.file_offset),
            );
            frame.render_stateful_widget(
                List::new(items)
                    .block(Block::default().borders(Borders::ALL).title("Files"))
                    .highlight_symbol("> ")
                    .highlight_style(Style::default().add_modifier(Modifier::BOLD)),
                pane,
                &mut state,
            );
        }
        if wide || !self.files_focus {
            let pane = if wide {
                Rect::new(
                    body.x + file_width,
                    body.y,
                    body.width - file_width,
                    body.height,
                )
            } else {
                body
            };
            let title = self
                .files
                .get(self.selected)
                .map_or("Unstaged changes", |file| file.path.as_str());
            let block = Block::default().borders(Borders::ALL).title(Span::styled(
                title,
                Style::default().add_modifier(Modifier::BOLD),
            ));
            let inner = block.inner(pane);
            frame.render_widget(block, pane);
            if let Some(diff) = &self.diff {
                let selected_hunk = diff.rows.get(self.row).map(|row| row.hunk);
                let lines: Vec<Line<'static>> = diff
                    .rows
                    .iter()
                    .skip(self.row)
                    .take(usize::from(inner.height))
                    .map(|row| {
                        let (text, color) = match row.kind {
                            Kind::Header => {
                                let hunk = &diff.hunks[row.hunk];
                                (
                                    format!(
                                        "@@ -{},{} +{},{} @@",
                                        hunk.old.0, hunk.old.1, hunk.new.0, hunk.new.1
                                    ),
                                    Color::Cyan,
                                )
                            }
                            Kind::NoNewline => {
                                ("\\ No newline at end of file".into(), Color::Reset)
                            }
                            _ => {
                                let old = row.old.map_or_else(String::new, |line| line.to_string());
                                let new = row.new.map_or_else(String::new, |line| line.to_string());
                                let (sign, color) = match row.kind {
                                    Kind::Addition => ('+', Color::Green),
                                    Kind::Deletion => ('-', Color::Red),
                                    _ => (' ', Color::Reset),
                                };
                                (
                                    format!(
                                        "{old:>7} {new:>7} {sign} {}",
                                        output::line(diff.text(row))
                                    ),
                                    color,
                                )
                            }
                        };
                        let mut style = Style::default().fg(color);
                        if row.kind == Kind::Header && selected_hunk == Some(row.hunk) {
                            style = style.add_modifier(Modifier::BOLD);
                        }
                        Line::from(Span::styled(text, style))
                    })
                    .collect();
                frame.render_widget(Paragraph::new(lines).scroll((0, self.column)), inner);
            } else {
                frame.render_widget(Paragraph::new(self.message.as_str()), inner);
            }
        }
        let (mode, controls) = if self.files_focus {
            ("Mode: Files", "   Tab: Diff   j/k select file")
        } else {
            ("Mode: Diff", "   Tab: Files   j/k scroll diff")
        };
        frame.render_widget(
            Paragraph::new(vec![
                Line::raw(self.message.as_str()),
                Line::from(vec![
                    Span::raw("q quit   "),
                    Span::styled(mode, Style::default().add_modifier(Modifier::BOLD)),
                    Span::raw(controls),
                ]),
                Line::raw("r refresh  Esc cancel   [ ] hunks   PgUp/PgDn page   ←/→ scroll"),
            ]),
            status,
        );
    }
}

impl App {
    fn key(
        &mut self,
        key: event::KeyEvent,
        page: usize,
        worker: &mut Worker,
    ) -> Result<Option<u8>, Error> {
        let previous = self.selected;
        let rows = self.diff.as_ref().map_or(0, |diff| diff.rows.len());
        match key.code {
            KeyCode::Char('q') => return Ok(Some(0)),
            KeyCode::Char('c') if key.modifiers.contains(KeyModifiers::CONTROL) => {
                return Ok(Some(130));
            }
            KeyCode::Tab => self.files_focus = !self.files_focus,
            KeyCode::Esc if self.loading => {
                worker.cancel.cancel();
                self.pending = None;
                self.generation = self
                    .generation
                    .checked_add(1)
                    .ok_or(Error::Budget("operation generation"))?;
                self.loading = false;
                self.message = "Loading cancelled; r refreshes".into();
            }
            KeyCode::Char('r') => {
                self.files.clear();
                self._labels = None;
                self.selected = 0;
                self.file_offset = 0;
                self.request(worker, Request::Load)?;
            }
            KeyCode::Down | KeyCode::Char('j') if self.files_focus => {
                self.selected = (self.selected + 1).min(self.files.len().saturating_sub(1))
            }
            KeyCode::Up | KeyCode::Char('k') if self.files_focus => {
                self.selected = self.selected.saturating_sub(1)
            }
            KeyCode::Down | KeyCode::Char('j') => {
                self.row = (self.row + 1).min(rows.saturating_sub(1))
            }
            KeyCode::Up | KeyCode::Char('k') => self.row = self.row.saturating_sub(1),
            KeyCode::PageDown => self.row = (self.row + page).min(rows.saturating_sub(1)),
            KeyCode::PageUp => self.row = self.row.saturating_sub(page),
            KeyCode::Right => self.column = self.column.saturating_add(4).min(8192),
            KeyCode::Left => self.column = self.column.saturating_sub(4),
            KeyCode::Char(']') => {
                if let Some(diff) = &self.diff
                    && let Some(hunk) = diff.hunks.iter().find(|hunk| hunk.row > self.row)
                {
                    self.row = hunk.row;
                }
            }
            KeyCode::Char('[') => {
                if let Some(diff) = &self.diff
                    && let Some(hunk) = diff.hunks.iter().rev().find(|hunk| hunk.row < self.row)
                {
                    self.row = hunk.row;
                }
            }
            _ => {}
        }
        if previous != self.selected && self.files.get(self.selected).is_some() {
            self.request(worker, Request::Patch(self.selected))?;
        }
        Ok(None)
    }

    fn accept(&mut self, reply: Reply, worker: &mut Worker) -> Result<bool, Error> {
        let (generation, result) = reply;
        if generation != self.generation {
            return Ok(false);
        }
        self.loading = false;
        match result {
            Ok(ResultData::Loaded(files, reservation)) => {
                self.files = files;
                self._labels = Some(reservation);
                self.selected = 0;
                if self.files.is_empty() {
                    self.message = "No unstaged tracked changes".into();
                } else {
                    self.request(worker, Request::Patch(0))?;
                }
            }
            Ok(ResultData::Patch(diff)) => {
                let unavailable = self.files.iter().filter(|file| file.unavailable).count();
                self.message = format!(
                    "Files: {} | Hunks: {}{}",
                    self.files.len(),
                    diff.hunks.len(),
                    if unavailable > 0 {
                        " | incomplete: unavailable entries"
                    } else {
                        ""
                    }
                );
                self.diff = Some(diff);
            }
            Err(error) => {
                if let Some(file) = self.files.get_mut(self.selected) {
                    file.unavailable = true;
                }
                self.message = error.to_string();
            }
        }
        Ok(true)
    }
}

async fn event_loop(
    app: &mut App,
    terminal: &mut TerminalGuard,
    worker: &mut Worker,
    signals: &mut Signals,
) -> Result<u8, Error> {
    let mut tick = tokio::time::interval(Duration::from_millis(16));
    let mut dirty = true;
    loop {
        tokio::select! {
            biased;
            _ = signals.cancelled() => return Ok(130),
            _ = tick.tick() => {}
        }
        // Check the actual size: resize notifications can be coalesced with input.
        let area = terminal::area().map_err(|e| Error::io("read terminal size", e))?;
        if area != terminal.terminal.get_frame().area() {
            terminal
                .terminal
                .resize(area)
                .map_err(|e| Error::io("resize terminal", e))?;
            dirty = true;
        }
        if let Some(work) = app.pending.take() {
            match worker
                .send
                .as_ref()
                .ok_or(Error::Unavailable("worker stopped"))?
                .try_send(work)
            {
                Ok(()) => {}
                Err(TrySendError::Full(work)) => app.pending = Some(work),
                Err(TrySendError::Disconnected(_)) => {
                    return Err(Error::Unavailable("repository worker stopped"));
                }
            }
        }
        while let Ok(reply) = worker.receive.try_recv() {
            dirty |= app.accept(reply, worker)?;
        }
        if worker
            .handle
            .as_ref()
            .is_some_and(|handle| handle.is_finished())
        {
            return Err(Error::Unavailable("repository worker stopped"));
        }
        for _ in 0..32 {
            if !event::poll(Duration::ZERO).map_err(|e| Error::io("poll terminal input", e))? {
                break;
            }
            match event::read().map_err(|e| Error::io("read terminal input", e))? {
                Event::Key(key) if key.kind != KeyEventKind::Release => {
                    let page = usize::from(area.height.saturating_sub(5)).max(1);
                    if let Some(code) = app.key(key, page, worker)? {
                        return Ok(code);
                    }
                    dirty = true;
                }
                _ => {}
            }
        }
        if dirty {
            terminal
                .terminal
                .draw(|frame| app.draw(frame))
                .map_err(|e| Error::io("draw terminal", e))?;
            dirty = false;
        }
    }
}

async fn review(
    git: PathBuf,
    directory: PathBuf,
    config: crate::config::Config,
    signals: &mut Signals,
) -> Result<u8, Error> {
    use std::io::IsTerminal;
    if !std::io::stdin().is_terminal() || !std::io::stdout().is_terminal() {
        return Err(Error::Unavailable(
            "changes requires an interactive terminal",
        ));
    }
    let report = git::probe(git.clone(), &config, signals).await?;
    if report.version < git::MINIMUM {
        return Err(Error::Unavailable("Git 2.55.0 or newer is required"));
    }
    let message = format!("Git {}: {}\n", report.version, output::path(&git));
    let reporting = tokio::task::spawn_blocking(move || output::write(message.as_bytes(), true));
    tokio::select! {
        biased;
        _ = signals.cancelled() => return Err(Error::Cancelled),
        result = reporting => result.map_err(|_| Error::Unavailable("Git report failed"))?.map_err(|e| Error::io("report selected Git", e))?,
    }
    let mut terminal = TerminalGuard::new()?;
    let mut worker = Worker::new(git, directory)?;
    let mut app = App {
        files: Vec::new(),
        _labels: None,
        selected: 0,
        file_offset: 0,
        files_focus: true,
        diff: None,
        row: 0,
        column: 0,
        message: String::new(),
        generation: 0,
        pending: None,
        loading: false,
    };
    app.request(&mut worker, Request::Load)?;
    let result = event_loop(&mut app, &mut terminal, &mut worker, signals).await;
    let stopped = worker.shutdown();
    drop(terminal);
    stopped?;
    result
}

pub(crate) fn run(
    git: PathBuf,
    directory: PathBuf,
    config: crate::config::Config,
) -> Result<u8, Error> {
    let runtime = tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .max_blocking_threads(1)
        .build()
        .map_err(|e| Error::io("start UI runtime", e))?;
    let result = runtime.block_on(async {
        let mut signals = Signals::new().map_err(|e| Error::io("watch terminal signals", e))?;
        let result = review(git, directory, config, &mut signals).await;
        if matches!(result, Err(Error::Cancelled)) {
            return Ok(130);
        }
        let reporting = tokio::task::spawn_blocking(move || crate::report(result));
        Ok(tokio::select! {
            biased;
            _ = signals.cancelled() => 130,
            result = reporting => result.unwrap_or(1),
        })
    });
    runtime.shutdown_background();
    result
}
