package kv

// Delete deletes key-value pair specified by the key.
//   - if a network error happens, returns error.
func (ns *Namespace) Delete(key string) error {
	return ns.instance.Delete(key)
}
