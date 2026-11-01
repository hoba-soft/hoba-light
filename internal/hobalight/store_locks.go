package hobalight

func (s *Store) beginBackup() (func(), error)                { return s.backend.BeginWrite("backup") }
func (s *Store) beginWrite(operation string) (func(), error) { return s.backend.BeginWrite(operation) }
func (s *Store) beginGC() (func(), error)                    { return s.backend.BeginMaintenance("gc") }
func (s *Store) beginMaintenance(operation string) (func(), error) {
	return s.backend.BeginMaintenance(operation)
}
