package terminal

// Wrap splits s into rows of at most cols elements. An empty s yields a single
// empty row so blank lines still take up space on screen.
func Wrap[T any](s []T, cols int) [][]T {
	if cols < 1 {
		cols = 1
	}
	if len(s) <= cols {
		return [][]T{s}
	}
	rows := make([][]T, 0, len(s)/cols+1)
	for len(s) > cols {
		rows = append(rows, s[:cols:cols])
		s = s[cols:]
	}
	return append(rows, s)
}

// RowCount reports how many rows Wrap produces for n elements.
func RowCount(n, cols int) int {
	if cols < 1 {
		cols = 1
	}
	if n == 0 {
		return 1
	}
	return (n + cols - 1) / cols
}
