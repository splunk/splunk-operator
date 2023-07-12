package common

import "time"

type Perms struct {
	Read  []string `json:"read,omitempty"`
	Write []string `json:"write,omitempty"`
}

type ACL struct {
	App        string `json:"app,omitempty"`
	CanList    bool   `json:"can_list,omitempty"`
	CanWrite   bool   `json:"can_write,omitempty"`
	Modifiable bool   `json:"modifiable,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Perms      Perms  `json:"perms,omitempty"`
	Removable  bool   `json:"removable,omitempty"`
	Sharing    string `json:"sharing,omitempty"`
}

type HeaderLinks struct {
	Create string `json:"create,omitempty"`
	Reload string `json:"_reload,omitempty"`
	ACL    string `json:"_acl,omitempty"`
}

type Generator struct {
	Build   string `json:"build,omitempty"`
	Version string `json:"version,omitempty"`
}

type EntryLinks struct {
	Alternate string `json:"alternate,omitempty"`
	List      string `json:"list,omitempty"`
}

type Entry struct {
	Name     string      `json:"name,omitempty"`
	ID       string      `json:"id,omitempty"`
	Updated  time.Time   `json:"updated,omitempty"`
	Links    EntryLinks  `json:"links,omitempty"`
	Author   string      `json:"author,omitempty"`
	ACL      ACL         `json:"acl,omitempty"`
	Content  interface{} `json:"content,omitempty"`
	Content0 interface{} `json:"content0,omitempty"`
}

type Paging struct {
	Total   int `json:"total"`
	PerPage int `json:"perPage"`
	Offset  int `json:"offset"`
}
type Header struct {
	Links     HeaderLinks   `json:"links,omitempty"`
	Origin    string        `json:"origin,omitempty"`
	Updated   time.Time     `json:"updated,omitempty"`
	Generator Generator     `json:"generator,omitempty"`
	Entry     []Entry       `json:"acl,omitempty"`
	Paging    Paging        `json:"paging,omitempty"`
	Messages  []interface{} `json:"messages,omitempty"`
}
