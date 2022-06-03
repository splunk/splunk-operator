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
	"testing"
)

func TestLocalPath(t *testing.T) {

	src := localPath{file: "/tmp/test.text"}
	value := src.Base().file
	if value == "" {
		t.Errorf("unable to find base")
	}
	value = src.Dir().file
	if value == "" {
		t.Errorf("unable to find directory")
	}
	value = src.Clean().file
	if value == "" {
		t.Errorf("unable to clean file path")
	}
	value = src.StripSlashes().file
	if value == "" {
		t.Errorf("unable to strip slashes")
	}

	path := Path{
		basepath: "testing",
	}

	test1 := src.Join(path)
	if test1.file == "" {
		t.Errorf("unable to strip slashes")
	}

	_, err := src.Glob()
	if err != nil {
		t.Errorf("unable to Glob")
	}

}

type Path struct {
	basepath string
}

func (p Path) String() string {
	return p.basepath
}

func TestRemotePath(t *testing.T) {
	rp := remotePath{file: "/tmp/test.text"}
	value := rp.Base().file
	if value == "" {
		t.Errorf("unable to find base")
	}
	value = rp.Dir().file
	if value == "" {
		t.Errorf("unable to find directory")
	}
	value = rp.Clean().file
	if value == "" {
		t.Errorf("unable to clean file path")
	}
	value = rp.StripSlashes().file
	if value == "" {
		t.Errorf("unable to strip slashes")
	}

	path := Path{
		basepath: "testing",
	}

	test1 := rp.Join(path)
	if test1.file == "" {
		t.Errorf("unable to strip slashes")
	}

	rp = rp.StripShortcuts()
	if rp.file == "" {
		t.Errorf("unable to strip shortcuts")
	}
}
