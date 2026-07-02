package db

import (
	"fmt"
	"regexp"
	"strings"

	commonsql "github.com/yinstall/internal/common/sql"
)

// PDBSpec describes one CREATE PLUGGABLE DATABASE to run in CDB$ROOT after install.
type PDBSpec struct {
	Name            string
	AdminUser       string
	AdminPassword   string
	UsersDatafile   string
	UsersSize       string
	Archivelog      *bool
	CompatMode      string // empty or "mysql" (yashan omits COMPAT_MODE clause)
	FileConvertNone bool
	FileConvertFrom string
	FileConvertTo   string
	Open            bool
}

const (
	pdbParamName        = "name"
	pdbParamUser        = "user"
	pdbParamPassword    = "password"
	pdbParamDatafile    = "datafile"
	pdbParamSize        = "size"
	pdbParamArchivelog  = "archivelog"
	pdbParamCompat      = "compat"
	pdbParamFileConvert = "file_convert"
	pdbParamFileFrom    = "file_convert_from"
	pdbParamFileTo      = "file_convert_to"
	pdbParamOpen        = "open"

	// YashanDB official-style keys (CREATE PLUGGABLE DATABASE); mapped to short keys above.
	pdbOfficialAdminUser          = "admin_user"
	pdbOfficialAdminPassword      = "admin_password"
	pdbOfficialTablespaceDatafile = "tablespace_datafile"
	pdbOfficialTablespaceSize     = "tablespace_size"
	pdbOfficialCompatMode         = "compat_mode"
	pdbOfficialFileNameConvert    = "file_name_convert"

	defaultPDBAdminUser     = "admin"
	defaultPDBAdminPassword = "Yashan1!"
	defaultPDBUsersSize     = "128M"
	defaultPDBOpen          = true
)

// pdbSupportedKeys lists short keys and Yashan official aliases accepted on --db-pdb.
var pdbSupportedKeys = map[string]struct{}{
	pdbParamName: {}, pdbParamUser: {}, pdbParamPassword: {},
	pdbParamDatafile: {}, pdbParamSize: {}, pdbParamArchivelog: {},
	pdbParamCompat: {}, pdbParamFileConvert: {}, pdbParamOpen: {},
	pdbParamFileFrom: {}, pdbParamFileTo: {},
	pdbOfficialAdminUser: {}, pdbOfficialAdminPassword: {},
	pdbOfficialTablespaceDatafile: {}, pdbOfficialTablespaceSize: {},
	pdbOfficialCompatMode: {}, pdbOfficialFileNameConvert: {},
}

var (
	pdbNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$#]*$`)
)

// ParsePDBSpecs parses --db-pdb entries (repeatable flag or pipe-separated in one value).
// Each entry may be a bare PDB name or comma-separated key=value pairs.
func ParsePDBSpecs(entries []string) ([]PDBSpec, error) {
	var raw []string
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		for _, part := range strings.Split(entry, "|") {
			part = strings.TrimSpace(part)
			if part != "" {
				raw = append(raw, part)
			}
		}
	}
	if len(raw) == 0 {
		return nil, nil
	}

	out := make([]PDBSpec, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for i, specStr := range raw {
		spec, err := parseOnePDBSpec(specStr, i+1)
		if err != nil {
			return nil, err
		}
		key := strings.ToUpper(spec.Name)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate pdb name %q", spec.Name)
		}
		seen[key] = struct{}{}
		out = append(out, spec)
	}
	return out, nil
}

func parseOnePDBSpec(specStr string, index int) (PDBSpec, error) {
	specStr = strings.TrimSpace(specStr)
	if specStr == "" {
		return PDBSpec{}, fmt.Errorf("invalid --db-pdb entry %d: empty", index)
	}
	// Bare name shorthand: --db-pdb PDB1
	if !strings.Contains(specStr, "=") {
		specStr = pdbParamName + "=" + specStr
	}

	spec := PDBSpec{
		AdminUser:     defaultPDBAdminUser,
		AdminPassword: defaultPDBAdminPassword,
		UsersSize:     defaultPDBUsersSize,
		Open:          defaultPDBOpen,
	}
	kv := make(map[string]string)

	for _, part := range strings.Split(specStr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.Index(part, "=")
		if eq <= 0 {
			return PDBSpec{}, fmt.Errorf("invalid --db-pdb entry %d %q: expected key=value segments", index, specStr)
		}
		rawKey := strings.ToLower(strings.TrimSpace(part[:eq]))
		val := strings.TrimSpace(part[eq+1:])
		if rawKey == "" || val == "" {
			return PDBSpec{}, fmt.Errorf("invalid --db-pdb entry %d %q: empty key or value", index, specStr)
		}
		if _, ok := pdbSupportedKeys[rawKey]; !ok {
			return PDBSpec{}, fmt.Errorf("invalid --db-pdb entry %d: unknown key %q (short: name,user,password,datafile,size,file_convert,compat,archivelog,open; official: admin_user,admin_password,tablespace_datafile,tablespace_size,compat_mode,file_name_convert,file_convert_from,file_convert_to)", index, rawKey)
		}
		canonKey := canonicalPDBParamKey(rawKey)
		if _, exists := kv[canonKey]; exists {
			return PDBSpec{}, fmt.Errorf("invalid --db-pdb entry %d: duplicate key %q (conflicts with %q after alias resolution)", index, rawKey, canonKey)
		}
		kv[canonKey] = val
	}

	name := strings.TrimSpace(kv[pdbParamName])
	if name == "" {
		return PDBSpec{}, fmt.Errorf("invalid --db-pdb entry %d: %q is required", index, pdbParamName)
	}
	if !pdbNamePattern.MatchString(name) {
		return PDBSpec{}, fmt.Errorf("invalid --db-pdb entry %d: pdb name %q must match [A-Za-z_][A-Za-z0-9_$#]*", index, name)
	}
	spec.Name = name
	spec.UsersDatafile = "users_" + strings.ToLower(name)

	if v := strings.TrimSpace(kv[pdbParamUser]); v != "" {
		if !pdbNamePattern.MatchString(v) {
			return PDBSpec{}, fmt.Errorf("invalid --db-pdb entry %d: user %q must match [A-Za-z_][A-Za-z0-9_$#]*", index, v)
		}
		spec.AdminUser = v
	}
	if v := strings.TrimSpace(kv[pdbParamPassword]); v != "" {
		spec.AdminPassword = v
	}
	if v := strings.TrimSpace(kv[pdbParamDatafile]); v != "" {
		spec.UsersDatafile = v
	}
	if v := strings.TrimSpace(kv[pdbParamSize]); v != "" {
		spec.UsersSize = v
	}

	if v, ok := kv[pdbParamArchivelog]; ok {
		b, err := parseBoolKV(v, pdbParamArchivelog, index)
		if err != nil {
			return PDBSpec{}, err
		}
		spec.Archivelog = &b
	}

	if v := strings.TrimSpace(kv[pdbParamCompat]); v != "" {
		mode := strings.ToLower(v)
		switch mode {
		case "yashan":
			spec.CompatMode = ""
		case "mysql":
			spec.CompatMode = "mysql"
		default:
			return PDBSpec{}, fmt.Errorf("invalid --db-pdb entry %d: compat must be yashan or mysql", index)
		}
	}

	if v := strings.TrimSpace(kv[pdbParamFileConvert]); v != "" {
		if strings.TrimSpace(kv[pdbParamFileFrom]) != "" || strings.TrimSpace(kv[pdbParamFileTo]) != "" {
			return PDBSpec{}, fmt.Errorf("invalid --db-pdb entry %d: file_convert/file_name_convert cannot be combined with file_convert_from/file_convert_to", index)
		}
		none, from, to, err := parseFileConvertValue(v, index)
		if err != nil {
			return PDBSpec{}, err
		}
		if none {
			spec.FileConvertNone = true
		} else {
			spec.FileConvertFrom = from
			spec.FileConvertTo = to
		}
	} else {
		from := strings.TrimSpace(kv[pdbParamFileFrom])
		to := strings.TrimSpace(kv[pdbParamFileTo])
		switch {
		case from != "" && to != "":
			spec.FileConvertFrom = from
			spec.FileConvertTo = to
		case from != "" || to != "":
			return PDBSpec{}, fmt.Errorf("invalid --db-pdb entry %d: file_convert_from and file_convert_to must both be set", index)
		}
	}

	if v, ok := kv[pdbParamOpen]; ok {
		b, err := parseBoolKV(v, pdbParamOpen, index)
		if err != nil {
			return PDBSpec{}, err
		}
		spec.Open = b
	}

	return spec, nil
}

// canonicalPDBParamKey maps Yashan official --db-pdb keys to internal short keys.
func canonicalPDBParamKey(key string) string {
	switch key {
	case pdbOfficialAdminUser:
		return pdbParamUser
	case pdbOfficialAdminPassword:
		return pdbParamPassword
	case pdbOfficialTablespaceDatafile:
		return pdbParamDatafile
	case pdbOfficialTablespaceSize:
		return pdbParamSize
	case pdbOfficialCompatMode:
		return pdbParamCompat
	case pdbOfficialFileNameConvert:
		return pdbParamFileConvert
	case pdbParamFileFrom, pdbParamFileTo:
		return key
	default:
		return key
	}
}

// parseFileConvertValue parses file_convert: omitted elsewhere, none, or from:to (YFS/local paths).
func parseFileConvertValue(v string, index int) (none bool, from, to string, err error) {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "none") {
		return true, "", "", nil
	}
	sep := strings.Index(v, ":")
	if sep <= 0 || sep >= len(v)-1 {
		return false, "", "", fmt.Errorf("invalid --db-pdb entry %d: file_convert must be none or from:to (e.g. ?/containers/PDB$SEED:?/containers/pdb1 or +DG0/...:+DG1/...)", index)
	}
	return false, strings.TrimSpace(v[:sep]), strings.TrimSpace(v[sep+1:]), nil
}

func parseBoolKV(v, key string, index int) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid --db-pdb entry %d: %s must be true or false", index, key)
	}
}

// BuildPDBInstallSQL returns CREATE/ALTER statements for all PDB specs (root container).
func BuildPDBInstallSQL(specs []PDBSpec) (string, error) {
	createSQL, err := BuildPDBCreateSQL(specs)
	if err != nil {
		return "", err
	}
	openSQL := BuildPDBOpenSQL(PDBOpenTargetNames(specs))
	if openSQL == "" {
		return createSQL, nil
	}
	if createSQL == "" {
		return openSQL, nil
	}
	return createSQL + ";\n" + openSQL, nil
}

// BuildPDBCreateSQL returns CREATE PLUGGABLE DATABASE statements only.
func BuildPDBCreateSQL(specs []PDBSpec) (string, error) {
	if len(specs) == 0 {
		return "", nil
	}
	var stmts []string
	for _, spec := range specs {
		createSQL, err := buildCreatePDBSQL(spec)
		if err != nil {
			return "", err
		}
		stmts = append(stmts, createSQL)
	}
	return strings.Join(stmts, ";\n") + ";", nil
}

// BuildPDBOpenSQL returns ALTER PLUGGABLE DATABASE ... OPEN for the given PDB names.
func BuildPDBOpenSQL(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf("ALTER PLUGGABLE DATABASE %s OPEN;", strings.Join(names, ", "))
}

// PDBOpenTargetNames returns PDB names from specs with Open=true.
func PDBOpenTargetNames(specs []PDBSpec) []string {
	var names []string
	for _, spec := range specs {
		if spec.Open {
			names = append(names, spec.Name)
		}
	}
	return names
}

// PDBNamesNeedingOpen returns names from want that are not OPEN in status (NAME -> STATUS).
func PDBNamesNeedingOpen(want []string, status map[string]string) []string {
	if len(want) == 0 {
		return nil
	}
	var out []string
	for _, name := range want {
		if strings.EqualFold(pdbStatusForName(status, name), "OPEN") {
			continue
		}
		out = append(out, name)
	}
	return out
}

func pdbStatusForName(status map[string]string, name string) string {
	if v, ok := status[name]; ok {
		return v
	}
	for k, v := range status {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

func buildCreatePDBSQL(spec PDBSpec) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE PLUGGABLE DATABASE %s", spec.Name)

	switch {
	case spec.FileConvertNone:
		b.WriteString("\n  FILE_NAME_CONVERT=NONE")
	case spec.FileConvertFrom != "" && spec.FileConvertTo != "":
		fmt.Fprintf(&b, "\n  FILE_NAME_CONVERT=(%s, %s)",
			quoteSQLLiteral(spec.FileConvertFrom), quoteSQLLiteral(spec.FileConvertTo))
	}

	fmt.Fprintf(&b, "\n  ADMIN USER %s IDENTIFIED BY %s",
		spec.AdminUser, quoteSQLPassword(spec.AdminPassword))

	fmt.Fprintf(&b, "\n  DEFAULT TABLESPACE DATAFILE %s SIZE %s",
		quoteSQLLiteral(spec.UsersDatafile), spec.UsersSize)

	if spec.Archivelog != nil {
		if *spec.Archivelog {
			b.WriteString("\n  ARCHIVELOG")
		} else {
			b.WriteString("\n  NOARCHIVELOG")
		}
	}

	if spec.CompatMode == "mysql" {
		b.WriteString("\n  COMPAT_MODE = mysql")
	}

	return b.String(), nil
}

func quoteSQLLiteral(s string) string {
	return "'" + commonsql.EscapeSQLString(s) + "'"
}

// quoteSQLPassword quotes IDENTIFIED BY values per YashanDB rules: passwords with
// special characters (other than underscore) must use double quotes, not single quotes.
func quoteSQLPassword(s string) string {
	if sqlPasswordNeedsDoubleQuotes(s) {
		return `"` + s + `"`
	}
	return quoteSQLLiteral(s)
}

func sqlPasswordNeedsDoubleQuotes(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return true
	}
	return false
}
