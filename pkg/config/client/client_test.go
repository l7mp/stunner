package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseURI(t *testing.T) {
	// file
	u, err := getURI("file:///tmp/a")
	assert.NoError(t, err, "file URI parse")
	assert.Equal(t, "file", u.Scheme, "file URI scheme")
	assert.Equal(t, "", u.Host, "file URI host")
	assert.Equal(t, "/tmp/a", u.Path, "file URI path")

	// default is http
	u, err = getURI("/tmp/a")
	assert.NoError(t, err, "file URI parse")
	assert.Equal(t, "http", u.Scheme, "file URI scheme")
	assert.Equal(t, "", u.Host, "file URI host")
	assert.Equal(t, "/tmp/a", u.Path, "file URI path")

	// addr
	u, err = getURI("1.2.3.4:12345")
	assert.NoError(t, err, "file URI parse")
	assert.Equal(t, "http", u.Scheme, "file URI scheme")
	assert.Equal(t, "1.2.3.4:12345", u.Host, "file URI host")
	assert.Equal(t, "", u.Path, "file URI path")

	// addr+path
	u, err = getURI("1.2.3.4:12345/a/b")
	assert.NoError(t, err, "file URI parse")
	assert.Equal(t, "http", u.Scheme, "file URI scheme")
	assert.Equal(t, "1.2.3.4:12345", u.Host, "file URI host")
	assert.Equal(t, "/a/b", u.Path, "file URI path")

	// scheme+addr+path
	u, err = getURI("http://1.2.3.4:12345/a/b")
	assert.NoError(t, err, "file URI parse")
	assert.Equal(t, "http", u.Scheme, "file URI scheme")
	assert.Equal(t, "1.2.3.4:12345", u.Host, "file URI host")
	assert.Equal(t, "/a/b", u.Path, "file URI path")

	// ws+addr+path
	u, err = getURI("ws://1.2.3.4:12345/a/b")
	assert.NoError(t, err, "file URI parse")
	assert.Equal(t, "ws", u.Scheme, "file URI scheme")
	assert.Equal(t, "1.2.3.4:12345", u.Host, "file URI host")
	assert.Equal(t, "/a/b", u.Path, "file URI path")
}
