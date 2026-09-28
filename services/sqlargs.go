package services

import "fmt"

// argBinder hands out a parameter's placeholder as its value is bound.
//
// The search query is assembled from optional pieces, each contributing
// parameters, so the placeholders have to be numbered in the order the values
// end up in. Doing that by hand -- writing $%d against a counter and
// remembering to advance it by however many values the piece added -- means one
// missed increment silently renumbers every parameter after it. What comes back
// is not an error but the wrong answer: a state filter reading a bounding box's
// longitude, or Postgres refusing to infer a type for a parameter nothing
// mentions.
//
// Here the number cannot disagree with the position, because the placeholder is
// only ever produced by binding the value it stands for.
type argBinder struct {
	args []interface{}
}

// bind appends values and returns their placeholders in the same order, as
// []interface{} so they can be handed straight to fmt.Sprintf.
func (b *argBinder) bind(values ...interface{}) []interface{} {
	placeholders := make([]interface{}, len(values))
	for i, v := range values {
		b.args = append(b.args, v)
		placeholders[i] = fmt.Sprintf("$%d", len(b.args))
	}
	return placeholders
}

// one binds a single value and returns its placeholder, for the common case of
// a predicate with exactly one parameter.
func (b *argBinder) one(value interface{}) string {
	return b.bind(value)[0].(string)
}

// count is how many parameters have been bound so far. Taken as a mark once the
// WHERE clause is complete: the count query references those parameters and no
// others, and passing it one the statement never mentions is what made Postgres
// fail to infer a type for $1.
func (b *argBinder) count() int {
	return len(b.args)
}

// values are the bound values, in placeholder order.
func (b *argBinder) values() []interface{} {
	return b.args
}
