package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"

	"github.com/lxc/lxd/shared"
)

func containerFileHandler(d *Daemon, r *http.Request) Response {
	name := mux.Vars(r)["name"]
	c, err := containerLoadByName(d, name)
	if err != nil {
		return SmartError(err)
	}

	path := r.FormValue("path")
	if path == "" {
		return BadRequest(fmt.Errorf("missing path argument"))
	}

	switch r.Method {
	case "GET":
		return containerFileGet(c, path, r)
	case "POST":
		return containerFilePut(c, path, r)
	default:
		return NotFound
	}
}

func containerFileGet(c container, path string, r *http.Request) Response {
	/*
	 * Copy out of the ns to a temporary file, and then use that to serve
	 * the request from. This prevents us from having to worry about stuff
	 * like people breaking out of the container root by symlinks or
	 * ../../../s etc. in the path, since we can just rely on the kernel
	 * for correctness.
	 */
	temp, err := ioutil.TempFile("", "lxd_forkgetfile_")
	if err != nil {
		return InternalError(err)
	}
	defer temp.Close()

	// Pul the file from the container
	uid, gid, mode, err := c.FilePull(path, temp.Name())
	if err != nil {
		return SmartError(err)
	}

	headers := map[string]string{
		"X-LXD-uid":  fmt.Sprintf("%d", uid),
		"X-LXD-gid":  fmt.Sprintf("%d", gid),
		"X-LXD-mode": fmt.Sprintf("%04o", mode),
	}

	// Make a file response struct
	files := make([]fileResponseEntry, 1)
	files[0].identifier = filepath.Base(path)
	files[0].path = temp.Name()
	files[0].filename = filepath.Base(path)

	return FileResponse(r, files, headers, true)
}

func containerFilePut(c container, path string, r *http.Request) Response {
	// Extract file ownership and mode from headers
	uid, gid, mode := shared.ParseLXDFileHeaders(r.Header)

	// Write file content to a tempfile
	temp, err := ioutil.TempFile("", "lxd_forkputfile_")
	if err != nil {
		return InternalError(err)
	}
	defer func() {
		temp.Close()
		os.Remove(temp.Name())
	}()

	_, err = io.Copy(temp, r.Body)
	if err != nil {
		return InternalError(err)
	}

	// Transfer the file into the container
	err = c.FilePush(temp.Name(), path, uid, gid, mode)
	if err != nil {
		return InternalError(err)
	}

	return EmptySyncResponse
}
