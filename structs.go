package main

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// StructVars represents a database table and supporting information.
type StructField struct {
	GoName       string
	GoType       string
	DBName       string
	DBType       string
	DBDefault    *string
	DBPrimaryKey bool
	DBAutoInc    bool
	DBNullable   bool

	// if GoType is a supported database/sql/driver.Value type,
	// leave these blank
	ScanName string // name of a local variable to use (ie str<GoName>)
	ScanType string // must be a supported driver.Value type (ie string)
}

// StructVars represents a database table and supporting information. This
// is used to generate a Go struct{} and supporting CRUD methods.
type StructVars struct {
	V                 string                 // mini-var name (ie "u" for User)
	TableName         string                 // bare_table
	TableRef          string                 // schema.bare_table
	StructName        string                 // singular CamelCase of TableName
	StructFields      map[string]StructField // keyed on db column name
	StructFieldsOrder []StructField          // because Go templates love to sort the map...
	PluralName        string                 // plural CamelCase of TableName
}

// JoinVars represent a foreign key relationship between two tables. This
// is used to generate methods to enumerate relations.
type JoinVars struct {
	Base        StructVars
	Other       StructVars
	foreignkeys []string // as Base.DBNames
}

////////

// Cols returns a comma-separated string of the database columns
func (s StructVars) Cols() string {
	r := []string{}
	for _, f := range s.StructFields {
		r = append(r, f.DBName)
	}
	return strings.Join(r, ", ")
}

// Fields returns a comma-separated string of the struct fields
func (s StructVars) Fields() string {
	r := []string{}
	for _, f := range s.StructFields {
		r = append(r, s.V+"."+f.GoName)
	}
	return strings.Join(r, ", ")
}

// WherePK returns the where clause for this PK
func (s StructVars) WherePK() string {
	pki := 1
	r := []string{}
	for _, f := range s.StructFields {
		if f.DBPrimaryKey {
			r = append(r, fmt.Sprintf("%s=$%d::%s", f.DBName, pki, f.DBType))
			pki += 1
		}
	}
	return strings.Join(r, " and ")
}

// FieldsPK returns the struct Fields for the PK
func (s StructVars) FieldsPK() string {
	r := []string{}
	for _, f := range s.StructFields {
		if f.DBPrimaryKey {
			r = append(r, s.V+"."+f.GoName)
		}
	}
	return strings.Join(r, ", ")
}

// WhereFK returns the where clause for the FK
func (j JoinVars) WhereFK() string {
	idx := 1
	r := []string{}
	for _, f := range j.Base.StructFields {
		for _, k := range j.foreignkeys {
			if k == f.DBName {
				r = append(r, fmt.Sprintf("%s=$%d::%s", f.DBName, idx, f.DBType))
				idx += 1
				break
			}
		}
	}
	return strings.Join(r, ", ")
}

// FieldsFK returns the struct Fields for the FK
func (j JoinVars) FieldsFK() string {
	r := []string{}
	for _, f := range j.Base.StructFields {
		for _, k := range j.foreignkeys {
			if k == f.DBName {
				r = append(r, j.Base.V+"."+f.GoName)
				break
			}
		}
	}
	return strings.Join(r, ", ")
}

// VarsTypesPK returns variable names and Go Types for the PK
func (s StructVars) VarsTypesPK() string {
	r := []string{}
	for _, f := range s.StructFields {
		if f.DBPrimaryKey {
			r = append(r, f.DBName+" "+f.GoType)
		}
	}
	return strings.Join(r, ", ")
}

// VarsPK returns variable names for the PK
func (s StructVars) VarsPK() string {
	r := []string{}
	for _, f := range s.StructFields {
		if f.DBPrimaryKey {
			r = append(r, f.DBName)
		}
	}
	return strings.Join(r, ", ")
}

// UpdateCols returns a comma-separated string of the database columns
func (s StructVars) UpdateCols() string {
	idx := 1
	for _, f := range s.StructFields {
		if f.DBPrimaryKey {
			idx += 1
		}
	}

	r := []string{}
	for _, f := range s.StructFields {
		r = append(r, fmt.Sprintf("%s=$%d::%s", f.DBName, idx, f.DBType))
		idx += 1
	}
	return strings.Join(r, ", ")
}

// UpdateFields returns a comma-separated string of the struct fields
func (s StructVars) UpdateFields() string {
	r := []string{}
	for _, f := range s.StructFields {
		r = append(r, s.V+"."+f.GoName)
	}
	return strings.Join(r, ", ")
}

// InsertPlaceholders returns the $-placeholders with NULLs for autoincs
func (s StructVars) InsertPlaceholders() string {
	idx := 1
	r := []string{}
	for _, f := range s.StructFields {
		if f.DBAutoInc {
			r = append(r, "DEFAULT")
		} else {
			r = append(r, fmt.Sprintf("$%d", idx))
		}
		idx += 1
	}
	return strings.Join(r, ",")
}

func (f StructField) ParseCode(deststruct string) (string, error) {
	tname := strings.Replace(f.GoType, ".", "_", -1)
	if tname[:1] == "*" {
		tname = tname[1:] + "_ptr"
	}
	tpl, err := template.ParseFiles("tpl/special.tpl")
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	tpl.ExecuteTemplate(buf, tname, &struct{ Dest, Src string }{Dest: deststruct + "." + f.GoName, Src: f.ScanName})
	return buf.String(), nil
}
