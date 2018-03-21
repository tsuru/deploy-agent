package docker

import (
	"bytes"

	"gopkg.in/check.v1"
)

func (s *S) TestErrorCheckWriter(c *check.C) {
	tests := []struct {
		msg []string
		err string
	}{
		{
			msg: []string{`
			{"status":"Pulling from tsuru/static","id":"latest"}
			something
			{invalid},
			{"other": "other"}
		`}},
		{
			msg: []string{`
			{"status":"Pulling from tsuru/static","id":"latest"}
			something
			{"errorDetail": {"message": "my err msg"}}
			{"other": "other"}
		`},
			err: `my err msg`,
		},
		{
			msg: []string{`
			{"status":"Pulling from tsuru/static","id":"latest"}
			something
			{"errorDetail": {"`, `message": "my err msg"}}
			{"other": "other"}
		`},
			err: `my err msg`,
		},
		{
			msg: []string{`
			{"status":"Pulling from tsuru/static","id":"latest"}
			something`, `
			{"errorDetail": {"message": "my err msg"}}`, `
			{"other": "other"}
		`},
			err: `my err msg`,
		},
		{
			msg: []string{`{"errorDetail": {"message"`, `: "my err msg"}}`},
			err: `my err msg`,
		},
		{
			msg: []string{`
{"error":`, ` "my err msg"}`},
			err: `my err msg`,
		},
	}
	for _, tt := range tests {
		buf := bytes.NewBuffer(nil)
		writer := errorCheckWriter{W: buf}
		var err error
		for _, msg := range tt.msg {
			_, err = writer.Write([]byte(msg))
			if err != nil {
				break
			}
		}
		if tt.err != "" {
			c.Assert(err, check.ErrorMatches, tt.err)
		} else {
			c.Assert(err, check.IsNil)
		}
	}
}
