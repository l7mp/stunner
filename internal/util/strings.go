package util

import (
	"sort"
)

// check is list contains element a
func Member(list []string, a string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

// Unique returns unique elements in a string slice
func Unique(s []string) []string {
	temp := make(map[string]bool)
	var result []string
	for _, str := range s {
		if _, ok := temp[str]; !ok {
			temp[str] = true
			result = append(result, str)
		}
	}
	sort.Strings(result)
	return result
}

// Diff computes the diff of two string slices a and b, returns a-b and b-a
func Diff(a, b []string) ([]string, []string) {
	a = Unique(a)
	b = Unique(b)

	temp := map[string]int{}
	for _, s := range a {
		temp[s]++
	}
	for _, s := range b {
		temp[s]--
	}

	var aminb, bmina []string
	for s, v := range temp {
		if v > 0 {
			aminb = append(aminb, s)
		} else if v < 0 {
			bmina = append(bmina, s)
		}
	}
	return aminb, bmina
}

// Remove removes a given element from string slice
func Remove(ss []string, e string) []string {
	ret := make([]string, len(ss))
	copy(ret, ss)
	for i, s := range ss {
		if s == e {
			ret = append(ret[:i], ret[i+1:]...)
			return ret
		}
	}

	return ret
}
