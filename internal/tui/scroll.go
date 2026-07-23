package tui

// scrollViewport bounds a line-oriented viewport to its content.
type scrollViewport struct {
	Offset  int
	End     int
	Visible int
	Total   int
}

func newScrollViewport(total, visible, offset int) scrollViewport {
	visible = max(1, visible)
	offset = clampScrollOffset(offset, total, visible)
	return scrollViewport{
		Offset:  offset,
		End:     min(offset+visible, total),
		Visible: visible,
		Total:   total,
	}
}

func scrollMaxOffset(total, visible int) int {
	return max(0, total-max(1, visible))
}

func clampScrollOffset(offset, total, visible int) int {
	return min(max(0, offset), scrollMaxOffset(total, visible))
}

func keepScrollIndexVisible(index, total, visible, offset int) int {
	if total == 0 {
		return 0
	}
	index = min(max(0, index), total-1)
	viewport := newScrollViewport(total, visible, offset)
	if index < viewport.Offset {
		return index
	}
	if index >= viewport.End {
		return clampScrollOffset(index-viewport.Visible+1, total, viewport.Visible)
	}
	return viewport.Offset
}
