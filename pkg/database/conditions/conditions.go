package conditions

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Upsert(list *[]metav1.Condition, c metav1.Condition) {
	i := -1
	for idx := range *list {
		if (*list)[idx].Type == c.Type {
			i = idx
			break
		}
	}
	c.LastTransitionTime = metav1.Now()
	if i >= 0 {
		(*list)[i] = c
	} else {
		*list = append(*list, c)
	}
}
