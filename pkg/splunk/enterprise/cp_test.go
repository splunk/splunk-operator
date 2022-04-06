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

//TestRecursiveTarEmptySrcDir test recursively tar empty directory
func TestRecursiveTarEmptySrcDir(t *testing.T) {

	// prepare temporary files to tar operation
	if err := os.MkdirAll("/tmp/src1", os.ModePerm); err != nil {
		t.Errorf(err.Error())
	}

	if err := os.MkdirAll("/tmp/dst/", os.ModePerm); err != nil {
		t.Errorf(err.Error())
	}

	// test recursive directories copy
	src := localPath{file: "/tmp/src1"}
	dest := remotePath{file: "/tmp/dst"}
	var discard io.Writer = io.Discard
	err := cpMakeTar(src, dest, discard)
	if err != nil {
		t.Errorf("copy tar file failed")
	}

	if err := os.RemoveAll("/tmp/src1"); err != nil {
		t.Errorf(err.Error())
	}

	if err := os.RemoveAll("/tmp/dst"); err != nil {
		t.Errorf(err.Error())
	}
}

// TestRecursiveTarSrcOneDir test recursiviley tar one src directory
func TestRecursiveTarSrcOneDir(t *testing.T) {
	// prepare temporary files to tar operation
	if err := os.MkdirAll("/tmp/src2", os.ModePerm); err != nil {
		t.Errorf(err.Error())
	}
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("/tmp/src2/filename-%d.txt", i)
		err := ioutil.WriteFile(path, []byte("Hello"), 0755)
		if err != nil {
			t.Errorf(fmt.Sprintf("Unable to write file: %s", err.Error()))
		}
	}
	if err := os.MkdirAll("/tmp/dst/", os.ModePerm); err != nil {
		t.Errorf(err.Error())
	}

	// test recursive directories copy
	src := localPath{file: "/tmp/src2"}
	dest := remotePath{file: "/tmp/dst"}
	var discard io.Writer = io.Discard
	err := cpMakeTar(src, dest, discard)
	if err != nil {
		t.Errorf("copy tar file failed")
	}

	if err := os.RemoveAll("/tmp/src2"); err != nil {
		t.Errorf(err.Error())
	}

	if err := os.RemoveAll("/tmp/dst"); err != nil {
		t.Errorf(err.Error())
	}
}

// TestRecursiveTarSrcAllDir test recursiviley tar all src directory
func TestRecursiveTarSrcAllDir(t *testing.T) {

	// prepare temporary files to tar operation
	if err := os.MkdirAll("/tmp/src3/a/b/c/d/e", os.ModePerm); err != nil {
		t.Errorf(err.Error())
	}
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("/tmp/src3/a/filename-%d.txt", i)
		err := ioutil.WriteFile(path, []byte("Hello"), 0755)
		if err != nil {
			t.Errorf(fmt.Sprintf("Unable to write file: %s", err.Error()))
		}
		path = fmt.Sprintf("/tmp/src3/a/b/filename-%d.txt", i)
		err = ioutil.WriteFile(path, []byte("Hello"), 0755)
		if err != nil {
			t.Errorf(fmt.Sprintf("Unable to write file: %v", err))
		}
		path = fmt.Sprintf("/tmp/src3/a/b/c/filename-%d.txt", i)
		err = ioutil.WriteFile(path, []byte("Hello"), 0755)
		if err != nil {
			t.Errorf(fmt.Sprintf("Unable to write file: %v", err))
		}
		path = fmt.Sprintf("/tmp/src3/a/b/c/d/filename-%d.txt", i)
		err = ioutil.WriteFile(path, []byte("Hello"), 0755)
		if err != nil {
			t.Errorf(fmt.Sprintf("Unable to write file: %v", err))
		}
	}

	// test recursive directories copy
	src := localPath{file: "/tmp/src3"}
	dest := remotePath{file: "/tmp/dst"}
	var discard io.Writer = io.Discard
	err := cpMakeTar(src, dest, discard)
	if err != nil {
		t.Errorf("copy tar file failed")
	}

	if err := os.RemoveAll("/tmp/src3"); err != nil {
		t.Errorf(err.Error())
	}

	if err := os.RemoveAll("/tmp/dst"); err != nil {
		t.Errorf(err.Error())
	}
}
