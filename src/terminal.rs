use crate::Error;
use crossterm::{
    cursor::{Hide, Show},
    execute,
    terminal::{self, EnterAlternateScreen, LeaveAlternateScreen},
};
use ratatui::{Terminal, TerminalOptions, Viewport, backend::CrosstermBackend, layout::Rect};
use rustix::fs::{OFlags, fcntl_getfl, fcntl_setfl};
use std::{
    fs::File,
    io::{self, IsTerminal, Write},
    os::fd::AsFd,
    time::{Duration, Instant},
};

pub(crate) struct Writer {
    file: File,
    flags: OFlags,
}

impl Writer {
    fn new(output: impl AsFd) -> io::Result<Self> {
        let file = File::from(rustix::io::dup(output)?);
        let flags = fcntl_getfl(&file)?;
        fcntl_setfl(&file, flags | OFlags::NONBLOCK)?;
        Ok(Self { file, flags })
    }
}

impl Write for Writer {
    fn write(&mut self, bytes: &[u8]) -> io::Result<usize> {
        let deadline = Instant::now() + Duration::from_millis(100);
        loop {
            match self.file.write(bytes) {
                Err(error)
                    if error.kind() == io::ErrorKind::WouldBlock && Instant::now() < deadline =>
                {
                    std::thread::sleep(Duration::from_millis(1))
                }
                result => return result,
            }
        }
    }
    fn flush(&mut self) -> io::Result<()> {
        Ok(())
    }
}

impl Drop for Writer {
    fn drop(&mut self) {
        let _ = fcntl_setfl(&self.file, self.flags);
    }
}

type PanicHook = Box<dyn Fn(&std::panic::PanicHookInfo<'_>) + Send + Sync + 'static>;

pub(crate) struct TerminalGuard {
    pub terminal: Terminal<CrosstermBackend<Writer>>,
    previous_hook: Option<PanicHook>,
}

fn restore() -> io::Result<()> {
    let raw = terminal::disable_raw_mode();
    let screen = Writer::new(io::stdout())
        .and_then(|mut writer| execute!(writer, Show, LeaveAlternateScreen));
    raw.and(screen)
}

pub(crate) fn area() -> io::Result<Rect> {
    let (width, height) = terminal::size()?;
    Ok(Rect::new(0, 0, width.min(240), height.min(100)))
}

impl TerminalGuard {
    pub fn new() -> Result<Self, Error> {
        if !io::stdin().is_terminal() || !io::stdout().is_terminal() {
            return Err(Error::Unavailable(
                "changes requires an interactive terminal",
            ));
        }
        let setup = || -> io::Result<Terminal<CrosstermBackend<Writer>>> {
            let mut writer = Writer::new(io::stdout())?;
            terminal::enable_raw_mode()?;
            execute!(writer, EnterAlternateScreen, Hide)?;
            Terminal::with_options(
                CrosstermBackend::new(writer),
                TerminalOptions {
                    viewport: Viewport::Fixed(area()?),
                },
            )
        };
        Self::setup(setup)
    }

    fn setup(
        setup: impl FnOnce() -> io::Result<Terminal<CrosstermBackend<Writer>>>,
    ) -> Result<Self, Error> {
        let previous_hook = std::panic::take_hook();
        std::panic::set_hook(Box::new(|_| {
            let _ = restore();
            let _ = Writer::new(io::stderr()).and_then(|mut writer| {
                writer.write_all(b"xunhen: internal error; terminal restoration attempted\n")
            });
        }));
        match setup() {
            Ok(terminal) => Ok(Self {
                terminal,
                previous_hook: Some(previous_hook),
            }),
            Err(error) => {
                let _ = restore();
                std::panic::set_hook(previous_hook);
                Err(Error::io("initialize terminal", error))
            }
        }
    }
}

impl Drop for TerminalGuard {
    fn drop(&mut self) {
        let _ = restore();
        if !std::thread::panicking()
            && let Some(hook) = self.previous_hook.take()
        {
            std::panic::set_hook(hook);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::test_pty::Pty;
    use std::process::Command;

    #[test]
    fn panic_and_partial_setup_restore_terminal_modes() {
        if let Some(mode) = std::env::var_os("XUNHEN_TERMINAL_TEST") {
            if mode == "panic" {
                assert!(
                    std::panic::catch_unwind(|| {
                        let _guard = TerminalGuard::new().unwrap();
                        panic!("synthetic terminal failure");
                    })
                    .is_err()
                );
            } else {
                let result = TerminalGuard::setup(|| {
                    terminal::enable_raw_mode()?;
                    execute!(io::stdout(), EnterAlternateScreen, Hide)?;
                    Err(io::Error::other("synthetic setup failure"))
                });
                assert!(matches!(
                    result,
                    Err(Error::Io {
                        operation: "initialize terminal",
                        ..
                    })
                ));
            }
            return;
        }
        for mode in ["panic", "setup"] {
            let mut command = Command::new(std::env::current_exe().unwrap());
            command
                .args([
                    "--exact",
                    "terminal::tests::panic_and_partial_setup_restore_terminal_modes",
                    "--nocapture",
                ])
                .env("XUNHEN_TERMINAL_TEST", mode)
                .env("TERM", "xterm-256color");
            let mut terminal = Pty::spawn(command);
            assert!(terminal.finish().success());
        }
    }
}
