// Package pty opens Linux pseudo-terminal pairs for tests and measurement
// tools that run the command as a user's terminal would. The command never
// imports it.
package pty

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Pair is a pseudo-terminal. A child process gets Slave as its terminal; the
// caller writes keys to Master and reads the output from it.
type Pair struct {
	Master, Slave *os.File
}

// Open creates a pair and sets its size.
func Open(columns, rows uint16) (*Pair, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, fmt.Errorf("open a pseudo-terminal: %w", err)
	}

	var unlock int32
	if err := ioctl(master, syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		_ = master.Close()
		return nil, fmt.Errorf("unlock the pseudo-terminal: %w", err)
	}
	var number uint32
	if err := ioctl(master, syscall.TIOCGPTN, unsafe.Pointer(&number)); err != nil {
		_ = master.Close()
		return nil, fmt.Errorf("name the pseudo-terminal: %w", err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()
		return nil, fmt.Errorf("open the pseudo-terminal's slave side: %w", err)
	}

	p := &Pair{Master: master, Slave: slave}
	if err := p.Resize(columns, rows); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}

// Resize sets the terminal's size. The kernel sends SIGWINCH to the
// terminal's foreground process group.
func (p *Pair) Resize(columns, rows uint16) error {
	size := struct{ rows, columns, x, y uint16 }{rows: rows, columns: columns}
	if err := ioctl(p.Master, syscall.TIOCSWINSZ, unsafe.Pointer(&size)); err != nil {
		return fmt.Errorf("resize the pseudo-terminal: %w", err)
	}
	return nil
}

// Settings returns the terminal's line settings, which a program in raw mode
// changes and must put back.
func (p *Pair) Settings() (syscall.Termios, error) {
	var attrs syscall.Termios
	if err := ioctl(p.Slave, syscall.TCGETS, unsafe.Pointer(&attrs)); err != nil {
		return attrs, fmt.Errorf("read the terminal settings: %w", err)
	}
	return attrs, nil
}

// Close closes both sides.
func (p *Pair) Close() error {
	return errors.Join(p.Slave.Close(), p.Master.Close())
}

// Attach makes the pair the controlling terminal of a new session for the
// child that SysProcAttr describes, as a login shell's child would have.
func Attach() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
}

func ioctl(f *os.File, request uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), request, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}
