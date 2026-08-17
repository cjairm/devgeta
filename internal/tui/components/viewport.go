package tuicomponents

// VisibleWindow returns [start, end) into a rowsLen-length list such that the
// window has at most height rows and always contains cursor.
func VisibleWindow(rowsLen, cursor, height int) (start, end int) {
	if rowsLen <= height {
		return 0, rowsLen
	}
	start = cursor - height/2
	if start < 0 {
		start = 0
	}
	end = start + height
	if end > rowsLen {
		end = rowsLen
		start = end - height
		if start < 0 {
			start = 0
		}
	}
	return start, end
}
