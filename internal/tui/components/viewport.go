package tuicomponents

// VisibleWindow returns [start, end) into a rowsLen-length list such that the
// window has at most viewportHeight rows and always contains cursor.
func VisibleWindow(rowsLen, cursor, viewportHeight int) (start, end int) {
	if rowsLen <= viewportHeight {
		return 0, rowsLen
	}
	start = cursor - viewportHeight/2
	if start < 0 {
		start = 0
	}
	end = start + viewportHeight
	if end > rowsLen {
		end = rowsLen
		start = end - viewportHeight
		if start < 0 {
			start = 0
		}
	}
	return start, end
}
