package record

type Record struct {
	ID      int64
	Ts      int64
	Dir     string
	Session string
	Cmd     string
}
