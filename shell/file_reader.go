package shell

import "os"

// FileReader abstracts file reading for testability.
type FileReader interface {
	ReadFile(path string) (string, error)
}

// OSFileReader reads files from the OS filesystem.
type OSFileReader struct{}

func (r *OSFileReader) ReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
