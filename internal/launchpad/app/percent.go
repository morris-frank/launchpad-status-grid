package app

import "fmt"

// percent is a percentage right-aligned in three digits and its sign, so the
// words after it in a pad's description hold still as the value changes width.
func percent(n int) string { return fmt.Sprintf("%3d%%", n) }
