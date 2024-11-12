package util

import (
	"net"
	"os"
	"time"
)

type FileConnAddr struct {
	File *os.File
}

func (s *FileConnAddr) Network() string { return "file" }
func (s *FileConnAddr) String() string  { return s.File.Name() }

type FileConn struct {
	file *os.File
}

func (f *FileConn) Read(b []byte) (n int, err error) {
	return f.file.Read(b)
}

func (f *FileConn) Write(b []byte) (n int, err error) {
	return f.file.Write(b)
}

func (f *FileConn) Close() error {
	return f.file.Close()
}

func (f *FileConn) LocalAddr() net.Addr {
	return &FileConnAddr{File: f.file}
}

func (f *FileConn) RemoteAddr() net.Addr {
	return &FileConnAddr{File: f.file}
}

func (f *FileConn) SetDeadline(t time.Time) error {
	return nil
}

func (f *FileConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (f *FileConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// NewFileConn returns a wrapper that shows an os.File as a net.Conn.
func NewFileConn(file *os.File) net.Conn {
	return &FileConn{file: file}
}
