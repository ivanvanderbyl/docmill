package document

// HasUnsavedChanges reports edits relative to the original input bytes.
func (d *Document) HasUnsavedChanges() bool {
	return d != nil && d.changed.Load()
}
