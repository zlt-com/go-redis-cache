package redcache

type Condition struct {
	// Field   string
	// Value   interface{}
	Model    interface{}
	Instance interface{}
	Offset   interface{}
	Limit    interface{}
	OrderBy  interface{}
	Where    map[string]interface{}
}
