package docker

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

type errorCheckWriter struct {
	W io.Writer
	b []byte
}

type dockerJSONMessage struct {
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errorDetail,omitempty"`
	ErrorMessage string `json:"error"`
}

func (w *errorCheckWriter) Write(data []byte) (n int, err error) {
	n, err = w.W.Write(data)
	if err != nil {
		return
	}
	w.b = append(w.b, data...)
	if len(w.b) == 0 {
		return
	}
	parts := bytes.Split(w.b, []byte("\n"))
	w.b = parts[len(parts)-1]
	var msg dockerJSONMessage
	for _, part := range parts {
		jsonErr := json.Unmarshal(part, &msg)
		if jsonErr != nil {
			continue
		}
		if msg.Error != nil {
			return 0, errors.New(msg.Error.Message)
		}
		if msg.ErrorMessage != "" {
			return 0, errors.New(msg.ErrorMessage)
		}
	}
	return
}
