// Copyright 2012 Gary Burd
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

package redis_test

import (
	"bufio"
	"bytes"
	"errors"
	"github.com/garyburd/redigo/redis"
	"reflect"
	"strings"
	"testing"
)

var sendTests = []struct {
	args     []interface{}
	expected string
}{
	{
		[]interface{}{"SET", "foo", "bar"},
		"*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n",
	},
	{
		[]interface{}{"SET", "foo", "bar"},
		"*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n",
	},
	{
		[]interface{}{"SET", "foo", 100},
		"*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\n100\r\n",
	},
	{
		[]interface{}{"SET", "", []byte("foo")},
		"*3\r\n$3\r\nSET\r\n$0\r\n\r\n$3\r\nfoo\r\n",
	},
	{
		[]interface{}{"SET", nil, []byte("foo")},
		"*3\r\n$3\r\nSET\r\n$0\r\n\r\n$3\r\nfoo\r\n",
	},
}

func TestSend(t *testing.T) {
	for _, tt := range sendTests {
		var buf bytes.Buffer
		rw := bufio.ReadWriter{Writer: bufio.NewWriter(&buf)}
		c := redis.NewConnBufio(rw)
		err := c.Send(tt.args[0].(string), tt.args[1:]...)
		if err != nil {
			t.Errorf("Send(%v) returned error %v", tt.args, err)
			continue
		}
		rw.Flush()
		actual := buf.String()
		if actual != tt.expected {
			t.Errorf("Send(%v) = %q, want %q", tt.args, actual, tt.expected)
		}
	}
}

var errorSentinel = &struct{}{}

var receiveTests = []struct {
	reply    string
	expected interface{}
}{
	{
		"+OK\r\n",
		"OK",
	},
	{
		"@OK\r\n",
		errorSentinel,
	},
	{
		"$6\r\nfoobar\r\n",
		[]byte("foobar"),
	},
	{
		"$-1\r\n",
		nil,
	},
	{
		":1\r\n",
		int64(1),
	},
	{
		"*0\r\n",
		[]interface{}{},
	},
	{
		"*-1\r\n",
		nil,
	},
	{
		"*4\r\n$3\r\nfoo\r\n$3\r\nbar\r\n$5\r\nHello\r\n$5\r\nWorld\r\n",
		[]interface{}{[]byte("foo"), []byte("bar"), []byte("Hello"), []byte("World")},
	},
	{
		"*3\r\n$3\r\nfoo\r\n$-1\r\n$3\r\nbar\r\n",
		[]interface{}{[]byte("foo"), nil, []byte("bar")},
	},
}

func TestReceive(t *testing.T) {
	for _, tt := range receiveTests {
		rw := bufio.ReadWriter{
			Reader: bufio.NewReader(strings.NewReader(tt.reply)),
			Writer: bufio.NewWriter(nil), // writer need to support Flush
		}
		c := redis.NewConnBufio(rw)
		actual, err := c.Receive()
		if tt.expected == errorSentinel {
			if err == nil {
				t.Errorf("Receive(%q) did not return expected error", tt.reply)
			}
		} else {
			if err != nil {
				t.Errorf("Receive(%q) returned error %v", tt.reply, err)
				continue
			}
			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("Receive(%q) = %v, want %v", tt.reply, actual, tt.expected)
			}
		}
	}
}

type testConn struct {
	redis.Conn
}

func (t testConn) Close() error {
	_, err := t.Conn.Do("SELECT", "9")
	if err != nil {
		return nil
	}
	_, err = t.Conn.Do("FLUSHDB")
	if err != nil {
		return err
	}
	return t.Conn.Close()
}

func dial() (redis.Conn, error) {
	c, err := redis.Dial("tcp", ":6379")
	if err != nil {
		return nil, err
	}

	_, err = c.Do("SELECT", "9")
	if err != nil {
		return nil, err
	}

	n, err := redis.Int(c.Do("DBSIZE"))
	if err != nil {
		return nil, err
	}

	if n != 0 {
		return nil, errors.New("Database #9 is not empty, test can not continue")
	}

	return testConn{c}, nil
}

var testCommands = []struct {
	args     []interface{}
	expected interface{}
}{
	{
		[]interface{}{"PING"},
		"PONG",
	},
	{
		[]interface{}{"SET", "foo", "bar"},
		"OK",
	},
	{
		[]interface{}{"GET", "foo"},
		[]byte("bar"),
	},
	{
		[]interface{}{"GET", "nokey"},
		nil,
	},
	{
		[]interface{}{"MGET", "nokey", "foo"},
		[]interface{}{nil, []byte("bar")},
	},
	{
		[]interface{}{"INCR", "mycounter"},
		int64(1),
	},
	{
		[]interface{}{"LPUSH", "mylist", "foo"},
		int64(1),
	},
	{
		[]interface{}{"LPUSH", "mylist", "bar"},
		int64(2),
	},
	{
		[]interface{}{"LRANGE", "mylist", 0, -1},
		[]interface{}{[]byte("bar"), []byte("foo")},
	},
	{
		[]interface{}{"MULTI"},
		"OK",
	},
	{
		[]interface{}{"LRANGE", "mylist", 0, -1},
		"QUEUED",
	},
	{
		[]interface{}{"PING"},
		"QUEUED",
	},
	{
		[]interface{}{"EXEC"},
		[]interface{}{
			[]interface{}{[]byte("bar"), []byte("foo")},
			"PONG",
		},
	},
}

func TestDoCommands(t *testing.T) {
	c, err := dial()
	if err != nil {
		t.Fatalf("Error connection to database, %v", err)
	}
	defer c.Close()

	for _, cmd := range testCommands {
		actual, err := c.Do(cmd.args[0].(string), cmd.args[1:]...)
		if err != nil {
			t.Errorf("Do(%v) returned error %v", cmd.args, err)
			continue
		}
		if !reflect.DeepEqual(actual, cmd.expected) {
			t.Errorf("Do(%v) = %v, want %v", cmd.args, actual, cmd.expected)
		}
	}
}

func TestPipelineCommands(t *testing.T) {
	c, err := dial()
	if err != nil {
		t.Fatalf("Error connection to database, %v", err)
	}
	defer c.Close()

	for _, cmd := range testCommands {
		err := c.Send(cmd.args[0].(string), cmd.args[1:]...)
		if err != nil {
			t.Errorf("Send(%v) returned error %v", cmd.args, err)
			continue
		}
	}
	for _, cmd := range testCommands {
		actual, err := c.Receive()
		if err != nil {
			t.Errorf("Receive(%v) returned error %v", cmd.args, err)
			continue
		}
		if !reflect.DeepEqual(actual, cmd.expected) {
			t.Errorf("Receive(%v) = %v, want %v", cmd.args, actual, cmd.expected)
		}
	}
}