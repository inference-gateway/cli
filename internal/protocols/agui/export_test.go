package agui

import "io"

const OutboundQueue = outboundQueue

func NewQuietProbes(next io.Writer) io.Writer { return quietProbes{next: next} }
