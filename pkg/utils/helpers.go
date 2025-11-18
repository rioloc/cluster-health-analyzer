package utils

// Ptr is an helper function to return the pointer of any type
func Ptr[T any](v T) *T {
	return &v
}

// ReverseSlice reverses the elements of the given slice in place.
func ReverseSlice[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i] // Swap elements
	}
}
