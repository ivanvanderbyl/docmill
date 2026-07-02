package objects

// testHolder is a minimal IndirectObjectHolder for reference-resolution tests.
type testHolder struct {
	objs map[uint32]Object
}

func newTestHolder() *testHolder { return &testHolder{objs: map[uint32]Object{}} }

func (h *testHolder) GetOrParseIndirectObject(objNum uint32) Object { return h.objs[objNum] }

func (h *testHolder) add(objNum uint32, obj Object) {
	obj.SetObjNum(objNum)
	h.objs[objNum] = obj
}

// ints returns the integer value of each element via GetIntegerAt.
func ints(a *Array) []int {
	out := make([]int, a.Len())
	for i := range out {
		out[i] = a.GetIntegerAt(i)
	}
	return out
}
