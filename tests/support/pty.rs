use rustix::{
    fs::{OFlags, fcntl_getfl, fcntl_setfl},
    pty::{self, OpenptFlags},
    termios::{self, Winsize},
};
use std::{
    fs::File,
    io::{Read, Write},
    process::{Child, Command, ExitStatus, Stdio},
    time::{Duration, Instant},
};

pub struct Pty {
    pub child: Child,
    pub master: File,
    pub slave: File,
    initial: termios::Termios,
    pub parser: vt100::Parser,
    pub bytes: Vec<u8>,
}

impl Pty {
    pub fn spawn(mut command: Command) -> Self {
        let master =
            pty::openpt(OpenptFlags::RDWR | OpenptFlags::NOCTTY | OpenptFlags::CLOEXEC).unwrap();
        pty::grantpt(&master).unwrap();
        pty::unlockpt(&master).unwrap();
        let slave = File::from(
            pty::ioctl_tiocgptpeer(
                &master,
                OpenptFlags::RDWR | OpenptFlags::NOCTTY | OpenptFlags::CLOEXEC,
            )
            .unwrap(),
        );
        termios::tcsetwinsize(
            &slave,
            Winsize {
                ws_row: 28,
                ws_col: 120,
                ws_xpixel: 0,
                ws_ypixel: 0,
            },
        )
        .unwrap();
        let initial = termios::tcgetattr(&slave).unwrap();
        let master = File::from(master);
        fcntl_setfl(&master, fcntl_getfl(&master).unwrap() | OFlags::NONBLOCK).unwrap();
        let child = command
            .stdin(Stdio::from(slave.try_clone().unwrap()))
            .stdout(Stdio::from(slave.try_clone().unwrap()))
            .stderr(Stdio::from(slave.try_clone().unwrap()))
            .spawn()
            .unwrap();
        Self {
            child,
            master,
            slave,
            initial,
            parser: vt100::Parser::new(28, 120, 0),
            bytes: Vec::new(),
        }
    }
    pub fn drain(&mut self) {
        let mut buffer = [0u8; 8192];
        loop {
            match self.master.read(&mut buffer) {
                Ok(0) => break,
                Ok(n) => {
                    assert!(
                        self.bytes.len() + n <= 4 * 1024 * 1024,
                        "PTY output exceeded limit"
                    );
                    self.bytes.extend_from_slice(&buffer[..n]);
                    self.parser.process(&buffer[..n]);
                }
                Err(e)
                    if e.kind() == std::io::ErrorKind::WouldBlock
                        || e.raw_os_error() == Some(5) =>
                {
                    break;
                }
                Err(e) => panic!("PTY read: {e}"),
            }
        }
    }
    pub fn wait_for(&mut self, expected: &str) {
        self.wait_until(expected, |screen| screen.contents().contains(expected));
    }
    pub fn wait_until(&mut self, expected: &str, ready: impl Fn(&vt100::Screen) -> bool) {
        let deadline = Instant::now() + Duration::from_secs(15);
        loop {
            self.drain();
            if ready(self.parser.screen()) {
                return;
            }
            assert!(
                Instant::now() < deadline,
                "did not see {expected:?}: {}",
                self.parser.screen().contents()
            );
            assert!(
                self.child.try_wait().unwrap().is_none(),
                "child exited before {expected:?}: {}",
                String::from_utf8_lossy(&self.bytes)
            );
            std::thread::yield_now();
        }
    }
    pub fn send(&mut self, bytes: &[u8]) {
        self.master.write_all(bytes).unwrap();
    }
    pub fn finish(&mut self) -> ExitStatus {
        let deadline = Instant::now() + Duration::from_secs(10);
        loop {
            self.drain();
            if let Some(status) = self.child.try_wait().unwrap() {
                self.drain();
                let restored = termios::tcgetattr(&self.slave).unwrap();
                assert_eq!(restored.input_modes, self.initial.input_modes);
                assert_eq!(restored.output_modes, self.initial.output_modes);
                assert_eq!(restored.control_modes, self.initial.control_modes);
                assert_eq!(restored.local_modes, self.initial.local_modes);
                assert!(
                    !self.parser.screen().alternate_screen(),
                    "alternate screen was not restored"
                );
                return status;
            }
            assert!(Instant::now() < deadline, "child did not exit");
            std::thread::yield_now();
        }
    }
}

impl Drop for Pty {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}
