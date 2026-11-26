package handlers

import (
	"net/http"
)

// picNameList accepts an optional array of image filenames.
func picNameList(w http.ResponseWriter, m map[string]any) ([]string, bool) {
	v, exists := m["fileListName"]
	if !exists || v == nil {
		return []string{}, true
	}
	arr, ok := v.([]any)
	if !ok {
		write400(w, "fileListName 必须为图片文件名数组")
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		} else {
			write400(w, "fileListName 只能包含字符串")
			return nil, false
		}
	}
	return out, true
}
