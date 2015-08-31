package objectserver

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"time"
)

var RepUnmountedError = fmt.Errorf("Device unmounted")
var repDialer = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).Dial

const repITimeout = time.Minute * 10
const repOTimeout = time.Minute

type BeginReplicationRequest struct {
	Device     string
	Partition  string
	NeedHashes bool
}

type BeginReplicationResponse struct {
	Hashes map[string]string
}

type SyncFileRequest struct {
	Path   string
	Xattrs string
	Size   int64
}

type SyncFileResponse struct {
	Exists      bool
	NewerExists bool
	GoAhead     bool
	Msg         string
}

type FileUploadResponse struct {
	Success bool
	Msg     string
}

type RepConn struct {
	rw           *bufio.ReadWriter
	c            net.Conn
	Disconnected bool
}

func (r *RepConn) SendMessage(v interface{}) error {
	r.c.SetDeadline(time.Now().Add(repOTimeout))
	jsoned, err := json.Marshal(v)
	if err != nil {
		r.Close()
		return err
	}
	if err := binary.Write(r.rw, binary.BigEndian, uint32(len(jsoned))); err != nil {
		r.Close()
		return err
	}
	if _, err := r.rw.Write(jsoned); err != nil {
		r.Close()
		return err
	}
	if err := r.rw.Flush(); err != nil {
		r.Close()
		return err
	}
	return nil
}

func (r *RepConn) RecvMessage(v interface{}) (err error) {
	r.c.SetDeadline(time.Now().Add(repITimeout))
	var length uint32
	if err = binary.Read(r.rw, binary.BigEndian, &length); err != nil {
		r.Close()
		return
	}
	data := make([]byte, length)
	if _, err = io.ReadFull(r.rw, data); err != nil {
		r.Close()
		return
	}
	if err = json.Unmarshal(data, v); err != nil {
		r.Close()
		return
	}
	return
}

func (r *RepConn) Write(data []byte) (l int, err error) {
	r.c.SetDeadline(time.Now().Add(repOTimeout))
	if l, err = r.rw.Write(data); err != nil {
		r.Close()
	}
	return
}

func (r *RepConn) Flush() (err error) {
	r.c.SetDeadline(time.Now().Add(repOTimeout))
	if err = r.rw.Flush(); err != nil {
		r.Close()
	}
	return
}

func (r *RepConn) Read(data []byte) (l int, err error) {
	r.c.SetDeadline(time.Now().Add(repITimeout))
	if l, err = io.ReadFull(r.rw, data); err != nil {
		r.Close()
	}
	return
}

func (r *RepConn) Close() {
	r.Disconnected = true
	r.c.Close()
}

func NewRepConn(ip string, port int, device string, partition string) (*RepConn, error) {
	url := fmt.Sprintf("http://%s:%d/%s/%s", ip, port, device, partition)
	req, err := http.NewRequest("REPCONN", url, nil)
	if err != nil {
		return nil, err
	}
	conn, err := repDialer("tcp", req.URL.Host)
	if err != nil {
		return nil, err
	}
	hc := httputil.NewClientConn(conn, nil)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, RepUnmountedError
	}
	newc, _ := hc.Hijack()
	return &RepConn{rw: bufio.NewReadWriter(bufio.NewReader(newc), bufio.NewWriter(newc)), c: newc}, nil
}
