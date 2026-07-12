package metadata

// Result holds whatever metadata an extractor could resolve. Any field left
// empty means "not found by this extractor" -- callers fall back accordingly.
type Result struct {
	Title   string
	Author  string
	Year    string
	Subject string
}
