package tenfoot

import "strings"

// PrefetchOrder concatenates unique handles/ids in caller-supplied priority.
// Earlier groups win; later duplicates are dropped. CoverCache.Request and
// PresentationCache.Request fill the existing concurrency floor in this order:
// focus, page, next page, strip, attract.
func PrefetchOrder(groups ...[]string) []string {
	n := 0
	for _, group := range groups {
		n += len(group)
	}
	if n == 0 {
		return nil
	}
	out := make([]string, 0, n)
	seen := make(map[string]struct{}, n)
	for _, group := range groups {
		for _, item := range group {
			item = normalizeHandleOrID(item)
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeHandleOrID(value string) string {
	if handle := normalizeHandle(value); handle != "" {
		return handle
	}
	return strings.TrimSpace(value)
}
