package agui

import "io"

const (
	ProtocolVersion = protocolVersion
	OutboundQueue   = outboundQueue
)

func NewQuietProbes(next io.Writer) io.Writer { return quietProbes{next: next} }
