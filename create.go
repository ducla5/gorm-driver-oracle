package oracle

import (
	"bytes"
	"database/sql"
	"reflect"

	"github.com/thoas/go-funk"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"

	"github.com/ducla5/gorm-driver-oracle/clauses"
)

func Create(db *gorm.DB) {
	stmt := db.Statement
	sch := stmt.Schema
	boundVars := make(map[string]int)

	if stmt == nil || sch == nil {
		return
	}

	hasDefaultValues := len(sch.FieldsWithDefaultDBValue) > 0

	if !stmt.Unscoped {
		for _, c := range sch.CreateClauses {
			stmt.AddClause(c)
		}
	}

	if stmt.SQL.String() == "" {
		values := callbacks.ConvertToCreateValues(stmt)
		onConflict, hasConflict := stmt.Clauses["ON CONFLICT"].Expression.(clause.OnConflict)
		// are all columns in value the primary fields in schema only?
		if hasConflict && funk.Contains(
			funk.Map(values.Columns, func(c clause.Column) string { return c.Name }),
			funk.Map(sch.PrimaryFields, func(field *schema.Field) string { return field.DBName }),
		) {
			stmt.AddClauseIfNotExists(clauses.Merge{
				Using: []clause.Interface{
					clause.Select{
						Columns: funk.Map(values.Columns, func(column clause.Column) clause.Column {
							// HACK: I can not come up with a better alternative for now
							// I want to add a value to the list of variable and then capture the bind variable position as well
							buf := bytes.NewBufferString("")
							stmt.Vars = append(stmt.Vars, values.Values[0][funk.IndexOf(values.Columns, column)])
							stmt.BindVarTo(buf, stmt, nil)

							column.Alias = column.Name
							// then the captured bind var will be the name
							column.Name = buf.String()
							return column
						}).([]clause.Column),
					},
					clause.From{
						Tables: []clause.Table{{Name: db.Dialector.(Dialector).DummyTableName()}},
					},
				},
				On: funk.Map(sch.PrimaryFields, func(field *schema.Field) clause.Expression {
					return clause.Eq{
						Column: clause.Column{Table: stmt.Table, Name: field.DBName},
						Value:  clause.Column{Table: clauses.MergeDefaultExcludeName(), Name: field.DBName},
					}
				}).([]clause.Expression),
			})
			stmt.AddClauseIfNotExists(clauses.WhenMatched{Set: onConflict.DoUpdates})
			stmt.AddClauseIfNotExists(clauses.WhenNotMatched{Values: values})

			stmt.Build("MERGE", "WHEN MATCHED", "WHEN NOT MATCHED")
		} else {
			stmt.AddClauseIfNotExists(clause.Insert{Table: clause.Table{Name: stmt.Table}})
			stmt.AddClause(clause.Values{Columns: values.Columns, Values: [][]interface{}{values.Values[0]}})
			if hasDefaultValues {
				stmt.AddClauseIfNotExists(clause.Returning{
					Columns: funk.Map(sch.FieldsWithDefaultDBValue, func(field *schema.Field) clause.Column {
						return clause.Column{Name: field.DBName}
					}).([]clause.Column),
				})
			}
			stmt.Build("INSERT", "VALUES", "RETURNING")
			if hasDefaultValues {
				stmt.WriteString(" INTO ")
				for idx, field := range sch.FieldsWithDefaultDBValue {
					if idx > 0 {
						stmt.WriteByte(',')
					}
					boundVars[field.Name] = len(stmt.Vars)
					stmt.AddVar(stmt, sql.Out{Dest: reflect.New(field.FieldType).Interface()})
				}
			}
		}

		if !db.DryRun {
			for idx, vals := range values.Values {
				// HACK HACK: replace values one by one, assuming its value layout will be the same all the time, i.e. aligned
				for idx, val := range vals {
					switch v := val.(type) {
					case bool:
						if v {
							val = 1
						} else {
							val = 0
						}
					}

					stmt.Vars[idx] = val
				}
				// and then we insert each row one by one then put the returning values back (i.e. last return id => smart insert)
				// we keep track of the index so that the sub-reflected value is also correct

				// BIG BUG: what if any of the transactions failed? some result might already be inserted that oracle is so
				// sneaky that some transaction inserts will exceed the buffer and so will be pushed at unknown point,
				// resulting in dangling row entries, so we might need to delete them if an error happens

				switch result, err := stmt.ConnPool.ExecContext(stmt.Context, stmt.SQL.String(), stmt.Vars...); err {
				case nil: // success
					db.RowsAffected, _ = result.RowsAffected()

					insertTo := stmt.ReflectValue
					switch insertTo.Kind() {
					case reflect.Slice, reflect.Array:
						insertTo = insertTo.Index(idx)
					}

					if hasDefaultValues {
						// bind returning value back to reflected value in the respective fields
						funk.ForEach(
							funk.Filter(sch.FieldsWithDefaultDBValue, func(field *schema.Field) bool {
								return funk.Contains(boundVars, field.Name)
							}),
							func(field *schema.Field) {
								if err = field.Set(insertTo, stmt.Vars[boundVars[field.Name]].(sql.Out).Dest); err != nil {
									db.AddError(err)
								}
							},
						)
					}
				default: // failure
					db.AddError(err)
				}
			}
		}
	}
}
