package rtsp

// frameScanner reassembles concatenated JPEG frames (the ffmpeg mjpeg muxer
// output) from an arbitrary byte stream. JPEG entropy data can never
// contain 0xFFD9, so scanning for the SOI/EOI marker pair is a reliable
// frame delimiter.
type frameScanner struct {
	buffer []byte
}

// feed appends raw bytes and returns every complete frame found so far.
func (s *frameScanner) feed(chunk []byte) [][]byte {
	s.buffer = append(s.buffer, chunk...)
	var frames [][]byte
	for {
		start := indexOf(s.buffer, jpegSOI)
		if start < 0 {
			s.trim(keepTail(s.buffer, len(jpegSOI)))
			return frames
		}
		end := indexOf(s.buffer[start+2:], jpegEOI)
		if end < 0 {
			s.trim(start) // drop garbage before SOI, keep the partial frame
			return frames
		}
		frameEnd := start + 2 + end + len(jpegEOI)
		frame := make([]byte, frameEnd-start)
		copy(frame, s.buffer[start:frameEnd])
		frames = append(frames, frame)
		s.trim(frameEnd)
	}
}

// trim drops bytes from the buffer head.
func (s *frameScanner) trim(n int) {
	if n <= 0 {
		return
	}
	if n >= len(s.buffer) {
		s.buffer = s.buffer[:0]
		return
	}
	// copy in place to avoid unbounded growth
	copy(s.buffer, s.buffer[n:])
	s.buffer = s.buffer[:len(s.buffer)-n]
}

// keepTail returns how many trailing bytes to keep when no marker start was
// found: enough to recognize a marker split across chunks.
func keepTail(buffer []byte, markerLen int) int {
	if len(buffer) < markerLen {
		return len(buffer)
	}
	return markerLen - 1
}

var jpegSOI = []byte{0xFF, 0xD8}
var jpegEOI = []byte{0xFF, 0xD9}

func indexOf(haystack, needle []byte) int {
outer:
	for i := 0; i+len(needle) <= len(haystack); i++ {
		for j := range needle {
			if haystack[i+j] != needle[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}
