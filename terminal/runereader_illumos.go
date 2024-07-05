//go:build solaris && illumos
// +build solaris,illumos

package terminal

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
)

/*
#include <errno.h>
#include <stdio.h>
#include <stropts.h>
#include <termios.h>
#include <unistd.h>

int wrapper_set_term_mode(int fd, struct termios *t) {
  int res = ioctl(fd, TCSETS, t);
  if (res != 0) return errno;

  return 0;
}

int wrapper_tweak_term_mode(int fd, struct termios *t) {
  t->c_lflag &= ~(ECHO | ECHONL | ICANON | ISIG);
  t->c_cc[VMIN] = 1;
  t->c_cc[VTIME] = 0;

  int res = ioctl(fd, TCSETS, t);
  if (res != 0) return errno;

  return 0;
}

int wrapper_get_term_mode(int fd, struct termios *t) {
  int res = ioctl(fd, TCGETS, t);
  if (res != 0) return errno;

  return 0;
}
*/
import "C"

func getTerminalMode(fd uintptr, t *C.struct_termios) error {
	res := C.wrapper_get_term_mode(C.int(fd), t)
	if res != 0 {
		return fmt.Errorf("TCGETS ioctl failed with error code: %d", res)
	}
	return nil
}

func alterTerminalMode(fd uintptr, t *C.struct_termios) error {
	if res := C.wrapper_tweak_term_mode(C.int(fd), t); res != 0 {
		return fmt.Errorf("TCSETS ioctl failed with error code: %d", res)
	}
	return nil
}

func setTerminalMode(fd uintptr, t *C.struct_termios) error {
	if res := C.wrapper_set_term_mode(C.int(fd), t); res != 0 {
		return fmt.Errorf("TCSETS ioctl failed with error code: %d", res)
	}
	return nil
}

const (
	normalKeypad      = '['
	applicationKeypad = 'O'
)

type runeReaderState struct {
	term   C.struct_termios
	reader *bufio.Reader
	buf    *bytes.Buffer
}

func newRuneReaderState(input FileReader) runeReaderState {
	buf := new(bytes.Buffer)
	return runeReaderState{
		reader: bufio.NewReader(&BufferedReader{
			In:     input,
			Buffer: buf,
		}),
		buf: buf,
	}
}

func (rr *RuneReader) Buffer() *bytes.Buffer {
	return rr.state.buf
}

// For reading runes we just want to disable echo.
func (rr *RuneReader) SetTermMode() error {
	var tCurr C.struct_termios
	var tNew C.struct_termios
	var err error

	err = getTerminalMode(os.Stdin.Fd(), &tCurr)
	if err != nil {
		return err
	}

	// Persist current settings before we alter them.
	rr.state.term = tCurr

	// Make a copy of current settings and pass the copy to tweaking function.
	tNew = tCurr

	err = alterTerminalMode(os.Stdin.Fd(), &tNew)
	if err != nil {
		return err
	}

	return nil
}

func (rr *RuneReader) RestoreTermMode() error {
	if err := setTerminalMode(os.Stdin.Fd(), &rr.state.term); err != nil {
		return err
	}
	return nil
}

// ReadRune Parse escape sequences such as ESC [ A for arrow keys.
// See https://vt100.net/docs/vt102-ug/appendixc.html
func (rr *RuneReader) ReadRune() (rune, int, error) {
	r, size, err := rr.state.reader.ReadRune()
	if err != nil {
		return r, size, err
	}

	if r != KeyEscape {
		return r, size, err
	}

	if rr.state.reader.Buffered() == 0 {
		// no more characters so must be `Esc` key
		return KeyEscape, 1, nil
	}

	r, size, err = rr.state.reader.ReadRune()
	if err != nil {
		return r, size, err
	}

	// ESC O ... or ESC [ ...?
	if r != normalKeypad && r != applicationKeypad {
		return r, size, fmt.Errorf("unexpected escape sequence from terminal: %q", []rune{KeyEscape, r})
	}

	keypad := r

	r, size, err = rr.state.reader.ReadRune()
	if err != nil {
		return r, size, err
	}

	switch r {
	case 'A': // ESC [ A or ESC O A
		return KeyArrowUp, 1, nil
	case 'B': // ESC [ B or ESC O B
		return KeyArrowDown, 1, nil
	case 'C': // ESC [ C or ESC O C
		return KeyArrowRight, 1, nil
	case 'D': // ESC [ D or ESC O D
		return KeyArrowLeft, 1, nil
	case 'F': // ESC [ F or ESC O F
		return SpecialKeyEnd, 1, nil
	case 'H': // ESC [ H or ESC O H
		return SpecialKeyHome, 1, nil
	case '3': // ESC [ 3
		if keypad == normalKeypad {
			// discard the following '~' key from buffer
			_, _ = rr.state.reader.Discard(1)
			return SpecialKeyDelete, 1, nil
		}
	}

	// discard the following '~' key from buffer
	_, _ = rr.state.reader.Discard(1)
	return IgnoreKey, 1, nil
}
