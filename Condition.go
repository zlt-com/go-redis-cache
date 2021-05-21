package rediscache

type Condition struct {
	// Field   string
	// Value   interface{}
	Model    interface{}
	Instance interface{}
	Offset   int
	Limit    int
	OrderBy  string
	Where    map[string]interface{}
}
