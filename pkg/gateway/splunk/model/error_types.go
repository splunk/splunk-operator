package model

import ()

type SplunkError struct {
	Messages []struct {
		Type string `json:"type,omitempty"`
		Text string `json:"text,omitempty"`
	} `json:"messages,omitempty"`
}

func (s *SplunkError) Error() string {
	if len(s.Messages) > 0 {
		return s.Messages[0].Text
	}
	return "unknown error"
}
