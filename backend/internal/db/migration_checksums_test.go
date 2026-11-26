package db

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"yihe-care/migrations"
)

func digestSQL(text string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(text))) }

func TestMigrationChecksumsOnlyIgnoreNonLiteralLineEndings(t *testing.T) {
	lf := "-- don't parse 'quotes' in comments\nCREATE TABLE `example` (id bigint, note text DEFAULT 'it''s safe');\n/* regular \"comment\" */\n"
	crlf := strings.ReplaceAll(lf, "\n", "\r\n")
	for _, text := range []string{lf, crlf} {
		for _, stored := range []string{digestSQL(lf), digestSQL(crlf)} {
			if matches, err := migrationChecksumMatches([]byte(text), stored); err != nil || !matches {
				t.Fatal("non-literal newline variant rejected", err)
			}
		}
	}
	for _, changed := range []string{strings.Replace(lf, "bigint", "integer", 1), strings.Replace(lf, "example", "different", 1), strings.Replace(lf, "safe", "different", 1), strings.Replace(lf, "regular", "changed", 1), strings.Replace(lf, "id bigint", "id  bigint", 1), strings.ReplaceAll(lf, "\n", "\r")} {
		if matches, err := migrationChecksumMatches([]byte(changed), digestSQL(lf)); err != nil || matches {
			t.Fatal("a non-newline SQL change was accepted", err)
		}
	}
	escaped := "'escaped" + string(rune(92)) + "'\nsecond'"
	for _, literal := range []string{"'first\nsecond'", "\"first\nsecond\"", "`first\nsecond`", escaped, "'it''s\nsecond'"} {
		text := "SELECT " + literal + ";\n"
		changed := strings.ReplaceAll(text, "\n", "\r\n")
		if matches, err := migrationChecksumMatches([]byte(changed), digestSQL(text)); err != nil || matches {
			t.Fatal("literal/identifier newline change was accepted", err)
		}
	}
	for _, comment := range []string{"/*! SELECT 'a\nb' */\n", "/*+ HINT\n */\n"} {
		if matches, err := migrationChecksumMatches([]byte(strings.ReplaceAll(comment, "\n", "\r\n")), digestSQL(comment)); err != nil || matches {
			t.Fatal("executable comment/hint changed contents", err)
		}
	}
	// -- without following whitespace is an operator, not a MySQL comment.
	text := "SELECT 1--'a\nb';\n"
	if matches, err := migrationChecksumMatches([]byte(strings.ReplaceAll(text, "\n", "\r\n")), digestSQL(text)); err != nil || matches {
		t.Fatal("operator was treated as a comment", err)
	}
	for _, text := range []string{"SELECT 'unfinished", "/* unfinished"} {
		if _, err := migrationLineEndings(text, false); err == nil {
			t.Fatal("malformed SQL was normalized")
		}
	}
	text = "# prose with 'ignored quote\rSELECT 'a\nb';\n"
	if matches, err := migrationChecksumMatches([]byte(strings.ReplaceAll(text, "\n", "\r\n")), digestSQL(text)); err != nil || matches {
		t.Fatal("lone-CR comment hid a literal newline", err)
	}
}

func TestHistoricalMySQLMigrationNewlineForms(t *testing.T) {
	files, err := fs.Glob(migrations.Files, "mysql/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			data, err := migrations.Files.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			lf := strings.ReplaceAll(string(data), "\r\n", "\n")
			crlf := strings.ReplaceAll(lf, "\n", "\r\n")
			for _, input := range []string{lf, crlf} {
				for _, stored := range []string{digestSQL(lf), digestSQL(crlf)} {
					if matches, err := migrationChecksumMatches([]byte(input), stored); err != nil || !matches {
						t.Fatal("historical migration differs beyond safe newline conversion", err)
					}
				}
			}
		})
	}
}
