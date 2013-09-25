package main

/*
bdog - A reverse "go db" tool...

bdog converts an existing database schema into Go structs and methods.

Depluralization:
  "-ies" => "-y"   -- ontologies
  "-hes" => "h"    -- swatches
  "-oes" => "o"    -- potatoes
  "-s" => ""       -- flowers

TODO: preserve column ordering in Go output...

*/

import (
	"bytes"
	"database/sql"
	"flag"
	"fmt"
	_ "github.com/lib/pq"
	"go/format"
	"os"
	"os/user"
	"regexp"
	"strings"
	"text/template"
	"time"
)

// map from plural to singular
type DepluralizeMap struct {
	words map[string]string
}

/////////////

func (d *DepluralizeMap) String() string {
	var res []string
	for k, v := range d.words {
		res = append(res, k+":"+v)
	}
	return strings.Join(res, ",")
}

func (d *DepluralizeMap) Set(input string) error {
	if d.words == nil {
		d.words = make(map[string]string)
	}
	wordmaps := strings.Split(input, ",")
	for _, wm := range wordmaps {
		parts := strings.SplitN(wm, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("Invalid plural:singular map. Separate by colons then by commas. Ex: -d commas:comma,colons:colon")
		}
		d.words[strings.ToLower(parts[0])] = strings.ToLower(parts[1])
	}
	return nil
}

// SingleCamel converts underscore_names_with_plurals into
// UnderscoreNameWithPlural by converting all words to singular
// forms and concatenating the capitalized results
func SingleCamel(undered_word string) string {
	r1 := regexp.MustCompile("ies$")
	r2 := regexp.MustCompile("([ho])es$")
	r3 := regexp.MustCompile("([^s])s$")

	result := ""
	parts := strings.Split(undered_word, "_")
	for _, w := range parts {
		w = strings.ToLower(w)
		s, ok := depluralize.words[w]
		if !ok {
			s = r1.ReplaceAllLiteralString(w, "y")
			s = r2.ReplaceAllString(s, "$1")
			s = r3.ReplaceAllString(s, "$1")
		}
		result += strings.Title(s)
	}
	return result
}

// MultiCamel converts underscore_names_with_plurals into
// UnderscoreNamesWithPlurals by capitalizing and merging underscores
func MultiCamel(undered_word string) string {
	result := ""
	parts := strings.Split(undered_word, "_")
	for _, w := range parts {
		result += strings.Title(w)
	}
	return result
}

////////
// Postgresql-specific functions

type BDOGDatabase struct {
	db  *sql.DB
	buf bytes.Buffer

	tables   map[string]StructVars
	foreigns map[string]JoinVars

	useTime bool
	useNet  bool
}

var (
	COLS_SQL = `
    select table_schema, table_name, column_name, udt_name, is_nullable::bool, column_default
      from information_schema.columns
     where table_schema NOT IN ('pg_catalog','information_schema');`

	FK_SQL = `
	SELECT tc.constraint_name, tc.table_schema, tc.table_name, kcu.column_name,
		   ccu.table_schema as f_table_schema, ccu.table_name AS f_table_name, ccu.column_name AS f_column_name
	  FROM information_schema.table_constraints tc, information_schema.key_column_usage kcu,
	       information_schema.constraint_column_usage ccu
	 WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.constraint_name = kcu.constraint_name AND ccu.constraint_name = tc.constraint_name;`

	PK_SQL = `
	SELECT tc.table_schema, tc.table_name, kcu.column_name
	  FROM information_schema.table_constraints tc, information_schema.key_column_usage kcu
	 WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.constraint_name = kcu.constraint_name;`

	DATATYPE_MAP = map[string]string{
		"bool":        "bool",
		"bytea":       "[]byte",
		"int2":        "int16",
		"int4":        "int32",
		"int8":        "int64",
		"float4":      "float32",
		"float8":      "float64",
		"numeric":     "float64", // this REALLY needs a good replacement
		"money":       "float64", // this REALLY needs a good replacement
		"char":        "string",
		"varchar":     "string",
		"text":        "string",
		"xml":         "string",
		"uuid":        "string",
		"macaddr":     "net.HardwareAddr",
		"inet":        "net.IP", // technically this could be IPNet too
		"cidr":        "net.IPNet",
		"date":        "time.Time",
		"time":        "time.Time",
		"timestamp":   "time.Time",
		"timestamptz": "time.Time",
		"timetz":      "time.Time",

		/*
			"abstime":     "",
			"reltime":     "",
			"interval":    "",
			"tinterval":   "",
			"bit":         "",
			"varbit":      "",
			"tsvector":    "",
			"tsquery":     "",*/
	}
)

func (d *BDOGDatabase) Open(username, dbname string) (err error) {
	connstr := fmt.Sprintf("user='%s' dbname='%s' sslmode=disable", username, dbname)
	d.db, err = sql.Open("postgres", connstr)
	if err != nil {
		return err
	}

	d.useTime = false
	d.useNet = false
	d.tables = make(map[string]StructVars)
	d.foreigns = make(map[string]JoinVars)

	rows, err := d.db.Query(COLS_SQL)
	if err != nil {
		return err
	}

	for rows.Next() {
		var schema, name, column, datatype string
		var col_default *string // can be null
		var is_nullable bool

		err := rows.Scan(&schema, &name, &column, &datatype, &is_nullable, &col_default)
		if err != nil {
			return err
		}

		// hopefully we have a type mapping...
		gotype, hasgotype := DATATYPE_MAP[datatype]
		if !hasgotype {
			gotype = "sql.FIXME." + datatype
		}

		nullstar := ""
		if is_nullable {
			nullstar = "*"
		}

		tref := schema + "." + name
		sv, ok := d.tables[tref]
		if !ok {
			sv = StructVars{
				TableName:    name,
				TableRef:     tref,
				StructName:   SingleCamel(name),
				PluralName:   MultiCamel(name),
				V:            name[:1],
				StructFields: make(map[string]StructField),
			}
		}

		sf := StructField{
			GoName:     MultiCamel(column),
			GoType:     nullstar + gotype,
			DBName:     column,
			DBType:     datatype,
			DBNullable: is_nullable,
		}

		if col_default != nil && len(*col_default) > 10 && (*col_default)[:8] == "nextval(" {
			sf.DBAutoInc = true
			sf.DBDefault = nil
		} else {
			sf.DBAutoInc = false
			sf.DBDefault = col_default
		}

		if gotype == "net.IP" || gotype == "net.IPNet" || gotype == "net.HardwareAddr" {
			d.useNet = true
			sf.ScanName = "str" + sf.GoName
			sf.ScanType = nullstar + "string"
		} else {
			if gotype == "time.Time" {
				d.useTime = true
			}
			sf.ScanName = ""
		}

		sv.StructFields[sf.DBName] = sf
		d.tables[tref] = sv
	}

	if err := rows.Err(); err != nil {
		return err
	}

	//////
	// Primary Keys
	rows, err = d.db.Query(PK_SQL)
	if err != nil {
		return err
	}

	for rows.Next() {
		var schema, name, column string

		err := rows.Scan(&schema, &name, &column)
		if err != nil {
			return err
		}

		info := d.tables[schema+"."+name]
		for _, sf := range info.StructFields {
			if sf.DBName == column {
				sf.DBPrimaryKey = true
				info.StructFields[sf.DBName] = sf
			}
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	//////
	// Foreign Keys
	rows, err = d.db.Query(FK_SQL)
	if err != nil {
		return err
	}

	for rows.Next() {
		var fkname string
		var a_schema, a_name, a_column string
		var b_schema, b_name, b_column string

		err := rows.Scan(&fkname, &a_schema, &a_name, &a_column, &b_schema, &b_name, &b_column)
		if err != nil {
			return err
		}

		// overwriting is ok
		f := d.foreigns[fkname]
		f.Base = d.tables[a_schema+"."+a_name]
		f.Other = d.tables[b_schema+"."+b_name]

		// intermediate format for ordering...
		f.foreignkeys = append(f.foreignkeys, a_column+"."+b_column)
		d.foreigns[fkname] = f
	}

	if err := rows.Err(); err != nil {
		return err
	}

	// order foreign keys to match StructFields
	for fkname, jv := range d.foreigns {
		if len(jv.foreignkeys) == 1 {
			parts := strings.Split(jv.foreignkeys[0], ".")
			jv.foreignkeys = []string{parts[0]}
			continue
		}

		newfk := []string{}
		for dbname, sf := range jv.Other.StructFields {
			if !sf.DBPrimaryKey {
				continue
			}
			for _, rel := range jv.foreignkeys {
				parts := strings.Split(rel, ".")
				if parts[1] == dbname {
					newfk = append(newfk, parts[0])
				}
			}
		}
		f := d.foreigns[fkname]
		f.foreignkeys = newfk
	}

	// a hack because map enumeration order isn't respected in templates
	for tabpath, sv := range d.tables {
		for _, sf := range sv.StructFields {
			sv.StructFieldsOrder = append(sv.StructFieldsOrder, sf)
		}
		d.tables[tabpath] = sv
	}
	return nil
}

func GetTableNames(d *BDOGDatabase) (map[string]string, error) {
	nargs := flag.NArg()
	ntabs := len(os.Args) - nargs
	tabs := make(map[string]string, ntabs)

	if ntabs < len(os.Args) {
		// only do the listed tables
		for argi := ntabs; argi < len(os.Args); argi++ {
			// match selected table names to those found in the database
			for tabpath, info := range d.tables {
				if os.Args[argi] == tabpath || os.Args[argi] == info.TableName {
					tabs[tabpath] = info.StructName
				}
			}
		}
	} else {
		// use all table names found in the database
		for tabpath, info := range d.tables {
			tabs[tabpath] = info.StructName
		}
	}

	return tabs, nil
}

////////

func GetInitFile(d *BDOGDatabase) {
	usr, _ := user.Current()
	fmt.Fprintf(&d.buf, "// Generated by %s on %s\n", usr.Name, time.Now())
	fmt.Fprintf(&d.buf, "// using http://github.com/pbnjay/bdog\n//\n")

	fmt.Fprintf(&d.buf, "package %s\n\n", out_package)
	fmt.Fprintf(&d.buf, "import (\n \"database/sql\"\n _ \"github.com/lib/pq\"\n")
	fmt.Fprintf(&d.buf, ")\n\n")
	fmt.Fprintf(&d.buf, `
func init() {
	// FIXME: remove hard-coded connection params here
	connstr := "user='%s' dbname='%s' sslmode=disable"
	Db, err = sql.Open("postgres", connstr)
	if err != nil {
		return err
	}
}
		`, db_user, db_name)
}

//////////////

var (
	depluralize DepluralizeMap
	db_user     string
	db_name     string
	out_package string
)

func init() {
	flag.Var(&depluralize, "deplural", "optional map from plural to singular words. (words:word,others:other)")
	flag.StringVar(&db_user, "user", "(username)", "database username")
	flag.StringVar(&db_name, "name", "(dbname)", "database name")
	flag.StringVar(&out_package, "pkg", "models", "package name")

	// TODO: support custom column naming/capitalization through config file (YAML?)
	// TODO: support custom column type mapping through config file (YAML?)
	// TODO: output basic doc.go skeleton (option)

	// TODO: transparently support Many-to-Many relationships
	//       - either w/ no non-fk columns only,
	//			 - or w/ non-fk columns mapped to merged relation structs (embedded?)
}

func main() {
	flag.Parse()

	tpl, err := template.ParseFiles("tpl/bdog.tpl")
	if err != nil {
		fmt.Println(err)
		return
	}

	db := &BDOGDatabase{}
	err = db.Open(db_user, db_name)
	if err != nil {
		fmt.Println(err)
		return
	}

	usr, _ := user.Current()
	alsoneed := []string{}
	if db.useTime {
		alsoneed = append(alsoneed, "time")
	}
	if db.useNet {
		alsoneed = append(alsoneed, "net")
	}

	err = tpl.Execute(&db.buf, struct {
		Username     string
		Timestamp    time.Time
		PackageName  string
		OtherImports []string
		Tables       map[string]StructVars
		Joins        map[string]JoinVars
	}{
		Username:     usr.Name,
		Timestamp:    time.Now(),
		PackageName:  out_package,
		OtherImports: alsoneed,
		Tables:       db.tables,
		Joins:        db.foreigns,
	})

	if err != nil {
		fmt.Println(err)
	}

	pretty, err := format.Source(db.buf.Bytes())
	if err == nil {
		fmt.Print(string(pretty))
	} else {
		fmt.Print(string(db.buf.Bytes()))
	}
}
