//go:build linux

package main

// A small D-Bus client for the session bus, written against the wire format so the app stays standard
// library Go: enough to call methods, answer calls to the objects it exports (the tray icon and its
// menu) and send signals. Values are plain Go: byte, bool, int32, uint32, int64, uint64, float64,
// string, dbusPath, dbusSignature, dbusVariant, and []any for arrays, structs and dict entries.

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

type dbusPath string
type dbusSignature string

type dbusVariant struct {
	signature string
	value     any
}

type dbusMessage struct {
	kind        byte // 1 call, 2 return, 3 error, 4 signal
	serial      uint32
	replySerial uint32
	path        dbusPath
	iface       string
	member      string
	errorName   string
	destination string
	sender      string
	signature   string
	body        []any
}

type dbusConn struct {
	conn    net.Conn
	writer  sync.Mutex
	serial  uint32
	name    string
	waiting sync.Mutex
	replies map[uint32]chan *dbusMessage
	calls   chan *dbusMessage // method calls to our objects, and signals
	closed  chan struct{}
}

// connectSessionBus connects and says hello.
func connectSessionBus() (*dbusConn, error) {
	address := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if address == "" {
		if runtime := os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
			address = "unix:path=" + runtime + "/bus"
		}
	}
	var conn net.Conn
	var err error = errors.New("no session bus")
	for _, option := range strings.Split(address, ";") {
		transport, parameters, _ := strings.Cut(option, ":")
		if transport != "unix" {
			continue
		}
		for _, parameter := range strings.Split(parameters, ",") {
			key, value, _ := strings.Cut(parameter, "=")
			switch key {
			case "path":
				conn, err = net.Dial("unix", unescapeAddress(value))
			case "abstract":
				conn, err = net.Dial("unix", "@"+unescapeAddress(value))
			}
		}
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	c := &dbusConn{conn: conn, replies: map[uint32]chan *dbusMessage{}, calls: make(chan *dbusMessage, 32), closed: make(chan struct{})}
	if err := c.authenticate(); err != nil {
		conn.Close()
		return nil, err
	}
	go c.read()
	reply, err := c.call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "Hello", "")
	if err != nil {
		conn.Close()
		return nil, err
	}
	if len(reply) > 0 {
		c.name, _ = reply[0].(string)
	}
	return c, nil
}

func unescapeAddress(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '%' && i+2 < len(value) {
			if b, err := strconv.ParseUint(value[i+1:i+3], 16, 8); err == nil {
				out.WriteByte(byte(b))
				i += 2
				continue
			}
		}
		out.WriteByte(value[i])
	}
	return out.String()
}

func (c *dbusConn) authenticate() error {
	uid := hex.EncodeToString([]byte(strconv.Itoa(os.Getuid())))
	if _, err := c.conn.Write([]byte("\x00AUTH EXTERNAL " + uid + "\r\n")); err != nil {
		return err
	}
	line, err := bufio.NewReader(io.LimitReader(c.conn, 512)).ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(line, "OK ") {
		return fmt.Errorf("the session bus refused: %s", strings.TrimSpace(line))
	}
	_, err = c.conn.Write([]byte("BEGIN\r\n"))
	return err
}

func (c *dbusConn) close() { c.conn.Close() }

/* ---------- sending ---------- */

func (c *dbusConn) send(m *dbusMessage) (uint32, error) { return c.sendWaiting(m, nil) }

// sendWaiting sends a message; with waiting, its answer is delivered there.
func (c *dbusConn) sendWaiting(m *dbusMessage, waiting chan *dbusMessage) (uint32, error) {
	c.writer.Lock()
	defer c.writer.Unlock()
	c.serial++
	m.serial = c.serial
	data, err := encodeMessage(m)
	if err != nil {
		return 0, err
	}
	if waiting != nil {
		c.waiting.Lock()
		c.replies[m.serial] = waiting
		c.waiting.Unlock()
	}
	_, err = c.conn.Write(data)
	return m.serial, err
}

// call calls a method and waits for its answer.
func (c *dbusConn) call(destination string, path dbusPath, iface, member, signature string, args ...any) ([]any, error) {
	waiting := make(chan *dbusMessage, 1)
	m := &dbusMessage{kind: 1, destination: destination, path: path, iface: iface, member: member, signature: signature, body: args}
	if _, err := c.sendWaiting(m, waiting); err != nil {
		return nil, err
	}
	select {
	case reply := <-waiting:
		if reply.kind == 3 {
			text := reply.errorName
			if len(reply.body) > 0 {
				text += ": " + fmt.Sprint(reply.body[0])
			}
			return nil, errors.New(text)
		}
		return reply.body, nil
	case <-c.closed:
		return nil, errors.New("the session bus went away")
	}
}

// reply answers a method call.
func (c *dbusConn) reply(to *dbusMessage, signature string, values ...any) {
	c.send(&dbusMessage{kind: 2, replySerial: to.serial, destination: to.sender, signature: signature, body: values})
}

func (c *dbusConn) replyError(to *dbusMessage, name, text string) {
	c.send(&dbusMessage{kind: 3, replySerial: to.serial, destination: to.sender, errorName: name, signature: "s", body: []any{text}})
}

func (c *dbusConn) signal(path dbusPath, iface, member, signature string, values ...any) {
	c.send(&dbusMessage{kind: 4, path: path, iface: iface, member: member, signature: signature, body: values})
}

/* ---------- receiving ---------- */

func (c *dbusConn) read() {
	defer close(c.closed)
	reader := bufio.NewReader(c.conn)
	for {
		m, err := readMessage(reader)
		if err != nil {
			return
		}
		switch m.kind {
		case 2, 3:
			c.waiting.Lock()
			waiting := c.replies[m.replySerial]
			delete(c.replies, m.replySerial)
			c.waiting.Unlock()
			if waiting != nil {
				waiting <- m
			}
		default:
			select {
			case c.calls <- m:
			default:
			}
		}
	}
}

/* ---------- the wire format ---------- */

type dbusEncoder struct{ buffer []byte }

func (e *dbusEncoder) align(n int) {
	for len(e.buffer)%n != 0 {
		e.buffer = append(e.buffer, 0)
	}
}

func (e *dbusEncoder) u32(v uint32) {
	e.align(4)
	e.buffer = binary.LittleEndian.AppendUint32(e.buffer, v)
}

// value writes one value of a single complete type.
func (e *dbusEncoder) value(signature string, v any) error {
	switch signature[0] {
	case 'y':
		e.buffer = append(e.buffer, v.(byte))
	case 'b':
		b := uint32(0)
		if v.(bool) {
			b = 1
		}
		e.u32(b)
	case 'i':
		e.u32(uint32(v.(int32)))
	case 'u':
		e.u32(v.(uint32))
	case 'x', 't', 'd':
		e.align(8)
		var bits uint64
		switch n := v.(type) {
		case int64:
			bits = uint64(n)
		case uint64:
			bits = n
		case float64:
			bits = math.Float64bits(n)
		}
		e.buffer = binary.LittleEndian.AppendUint64(e.buffer, bits)
	case 's', 'o':
		var text string
		switch s := v.(type) {
		case string:
			text = s
		case dbusPath:
			text = string(s)
		}
		e.u32(uint32(len(text)))
		e.buffer = append(append(e.buffer, text...), 0)
	case 'g':
		text := string(v.(dbusSignature))
		e.buffer = append(append(append(e.buffer, byte(len(text))), text...), 0)
	case 'v':
		variant := v.(dbusVariant)
		if err := e.value("g", dbusSignature(variant.signature)); err != nil {
			return err
		}
		return e.value(variant.signature, variant.value)
	case 'a':
		element := signature[1:]
		e.u32(0)
		lengthAt := len(e.buffer) - 4
		e.align(alignment(element[0]))
		start := len(e.buffer)
		for _, item := range v.([]any) {
			if err := e.value(element, item); err != nil {
				return err
			}
		}
		binary.LittleEndian.PutUint32(e.buffer[lengthAt:], uint32(len(e.buffer)-start))
	case '(', '{':
		e.align(8)
		fields := splitSignature(signature[1 : len(signature)-1])
		values := v.([]any)
		if len(fields) != len(values) {
			return fmt.Errorf("struct %s has %d values", signature, len(values))
		}
		for i, field := range fields {
			if err := e.value(field, values[i]); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("can't send type %q", signature)
	}
	return nil
}

func alignment(code byte) int {
	switch code {
	case 'y', 'g', 'v':
		return 1
	case 'x', 't', 'd', '(', '{':
		return 8
	}
	return 4
}

// splitSignature splits a signature into its complete types.
func splitSignature(signature string) []string {
	var out []string
	for len(signature) > 0 {
		n := completeLength(signature)
		out = append(out, signature[:n])
		signature = signature[n:]
	}
	return out
}

func completeLength(signature string) int {
	switch signature[0] {
	case 'a':
		return 1 + completeLength(signature[1:])
	case '(', '{':
		depth := 0
		for i := 0; i < len(signature); i++ {
			switch signature[i] {
			case '(', '{':
				depth++
			case ')', '}':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
		}
	}
	return 1
}

func encodeMessage(m *dbusMessage) ([]byte, error) {
	body := &dbusEncoder{}
	for i, part := range splitSignature(m.signature) {
		if i >= len(m.body) {
			return nil, errors.New("too few values for the signature")
		}
		if err := body.value(part, m.body[i]); err != nil {
			return nil, err
		}
	}
	var fields []any
	add := func(code byte, signature string, value any) {
		fields = append(fields, []any{code, dbusVariant{signature, value}})
	}
	if m.path != "" {
		add(1, "o", m.path)
	}
	if m.iface != "" {
		add(2, "s", m.iface)
	}
	if m.member != "" {
		add(3, "s", m.member)
	}
	if m.errorName != "" {
		add(4, "s", m.errorName)
	}
	if m.replySerial != 0 {
		add(5, "u", m.replySerial)
	}
	if m.destination != "" {
		add(6, "s", m.destination)
	}
	if m.signature != "" {
		add(8, "g", dbusSignature(m.signature))
	}
	header := &dbusEncoder{buffer: []byte{'l', m.kind, 0, 1}}
	header.u32(uint32(len(body.buffer)))
	header.u32(m.serial)
	if err := header.value("a(yv)", fields); err != nil {
		return nil, err
	}
	header.align(8)
	return append(header.buffer, body.buffer...), nil
}

type dbusDecoder struct {
	data []byte
	at   int
}

var errShort = errors.New("short D-Bus message")

func (d *dbusDecoder) align(n int) {
	for d.at%n != 0 {
		d.at++
	}
}

func (d *dbusDecoder) u32() (uint32, error) {
	d.align(4)
	if d.at+4 > len(d.data) {
		return 0, errShort
	}
	v := binary.LittleEndian.Uint32(d.data[d.at:])
	d.at += 4
	return v, nil
}

func (d *dbusDecoder) value(signature string) (any, error) {
	switch signature[0] {
	case 'y':
		if d.at >= len(d.data) {
			return nil, errShort
		}
		d.at++
		return d.data[d.at-1], nil
	case 'b':
		v, err := d.u32()
		return v != 0, err
	case 'n', 'q':
		d.align(2)
		if d.at+2 > len(d.data) {
			return nil, errShort
		}
		d.at += 2
		return int32(int16(binary.LittleEndian.Uint16(d.data[d.at-2:]))), nil
	case 'i':
		v, err := d.u32()
		return int32(v), err
	case 'u', 'h':
		return d.u32()
	case 'x', 't', 'd':
		d.align(8)
		if d.at+8 > len(d.data) {
			return nil, errShort
		}
		bits := binary.LittleEndian.Uint64(d.data[d.at:])
		d.at += 8
		switch signature[0] {
		case 'x':
			return int64(bits), nil
		case 'd':
			return math.Float64frombits(bits), nil
		}
		return bits, nil
	case 's', 'o':
		length, err := d.u32()
		if err != nil || d.at+int(length)+1 > len(d.data) {
			return nil, errShort
		}
		text := string(d.data[d.at : d.at+int(length)])
		d.at += int(length) + 1
		if signature[0] == 'o' {
			return dbusPath(text), nil
		}
		return text, nil
	case 'g':
		if d.at >= len(d.data) {
			return nil, errShort
		}
		length := int(d.data[d.at])
		if d.at+1+length+1 > len(d.data) {
			return nil, errShort
		}
		text := string(d.data[d.at+1 : d.at+1+length])
		d.at += length + 2
		return dbusSignature(text), nil
	case 'v':
		inner, err := d.value("g")
		if err != nil {
			return nil, err
		}
		if inner.(dbusSignature) == "" {
			return nil, errShort
		}
		value, err := d.value(string(inner.(dbusSignature)))
		return dbusVariant{string(inner.(dbusSignature)), value}, err
	case 'a':
		length, err := d.u32()
		if err != nil {
			return nil, err
		}
		element := signature[1:]
		d.align(alignment(element[0]))
		end := d.at + int(length)
		if end > len(d.data) {
			return nil, errShort
		}
		items := []any{}
		for d.at < end {
			item, err := d.value(element)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	case '(', '{':
		d.align(8)
		var fields []any
		for _, field := range splitSignature(signature[1 : len(signature)-1]) {
			value, err := d.value(field)
			if err != nil {
				return nil, err
			}
			fields = append(fields, value)
		}
		return fields, nil
	}
	return nil, fmt.Errorf("can't read type %q", signature)
}

func readMessage(reader *bufio.Reader) (*dbusMessage, error) {
	fixed := make([]byte, 16)
	if _, err := io.ReadFull(reader, fixed); err != nil {
		return nil, err
	}
	if fixed[0] != 'l' {
		return nil, errors.New("big-endian D-Bus messages aren't supported")
	}
	bodyLength := binary.LittleEndian.Uint32(fixed[4:])
	fieldsLength := binary.LittleEndian.Uint32(fixed[12:])
	if bodyLength > 64<<20 || fieldsLength > 1<<20 {
		return nil, errors.New("D-Bus message too large")
	}
	headerLength := 16 + int(fieldsLength)
	total := (headerLength+7)&^7 + int(bodyLength)
	data := make([]byte, total)
	copy(data, fixed)
	if _, err := io.ReadFull(reader, data[16:]); err != nil {
		return nil, err
	}
	m := &dbusMessage{kind: fixed[1], serial: binary.LittleEndian.Uint32(fixed[8:])}
	header := &dbusDecoder{data: data[:headerLength], at: 12}
	fields, err := header.value("a(yv)")
	if err != nil {
		return nil, err
	}
	for _, field := range fields.([]any) {
		pair := field.([]any)
		value := pair[1].(dbusVariant).value
		switch pair[0].(byte) {
		case 1:
			m.path, _ = value.(dbusPath)
		case 2:
			m.iface, _ = value.(string)
		case 3:
			m.member, _ = value.(string)
		case 4:
			m.errorName, _ = value.(string)
		case 5:
			m.replySerial, _ = value.(uint32)
		case 6:
			m.destination, _ = value.(string)
		case 7:
			m.sender, _ = value.(string)
		case 8:
			signature, _ := value.(dbusSignature)
			m.signature = string(signature)
		}
	}
	body := &dbusDecoder{data: data[(headerLength+7)&^7:]}
	for _, part := range splitSignature(m.signature) {
		value, err := body.value(part)
		if err != nil {
			return nil, err
		}
		m.body = append(m.body, value)
	}
	return m, nil
}
