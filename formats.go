package main

type Format int

const (
	JSON Format = iota
	KeyValues
	NginxCombined
	ApacheCombined
	ApacheCommon
)
