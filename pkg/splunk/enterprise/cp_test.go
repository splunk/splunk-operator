// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"fmt"
	"io/ioutil"

	"io"
	"os"
	"testing"
)

func TestCpMakeTar(t *testing.T) {

	src := localPath{file: "test"}
	dest := remotePath{file: "test"}
	var discard io.Writer = io.Discard

	err := cpMakeTar(src, dest, discard)
	if err != nil {
		t.Errorf("copy failed")
	}
}

func TestRecursiveTar(t *testing.T) {
	// prepare temporary files to tar operation
	if err := os.MkdirAll("/tmp/src/a/b/c/d/e", os.ModePerm); err != nil {
		t.Errorf(err.Error())
	}
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("/tmp/src/a/filename-%d.txt", i)
		err := ioutil.WriteFile(path, []byte("Hello"), 0755)
		if err != nil {
			t.Errorf(fmt.Sprintf("Unable to write file: %s", err.Error()))
		}
		path = fmt.Sprintf("/tmp/src/a/b/filename-%d.txt", i)
		err = ioutil.WriteFile(path, []byte("Hello"), 0755)
		if err != nil {
			t.Errorf(fmt.Sprintf("Unable to write file: %v", err))
		}
		path = fmt.Sprintf("/tmp/src/a/b/c/filename-%d.txt", i)
		err = ioutil.WriteFile(path, []byte("Hello"), 0755)
		if err != nil {
			t.Errorf(fmt.Sprintf("Unable to write file: %v", err))
		}
		path = fmt.Sprintf("/tmp/src/a/b/c/d/filename-%d.txt", i)
		err = ioutil.WriteFile(path, []byte("Hello"), 0755)
		if err != nil {
			t.Errorf(fmt.Sprintf("Unable to write file: %v", err))
		}
	}
	if err := os.MkdirAll("/tmp/dst/", os.ModePerm); err != nil {
		t.Errorf(err.Error())
	}

	// test recursive directories copy
	src := localPath{file: "/tmp/src"}
	dest := remotePath{file: "/tmp/dst"}
	var discard io.Writer = io.Discard
	err := cpMakeTar(src, dest, discard)
	if err != nil {
		t.Errorf("copy tar file failed")
	}
}
