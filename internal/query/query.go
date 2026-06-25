package query

type Filter struct {
	TsFrom    int64
	TsTo      int64
	Dir       string
	Session   string
	CmdSubstr string
}
