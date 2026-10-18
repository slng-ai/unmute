package generate

// Lowering an SLNG-hosted tool. Shared by both code drivers, because the two
// things that make it work are the same on each: the module travels into the
// generated project, and the call goes through the platform's own contract
// rather than through a guess about the file's shape.
//
// A mirrored module is written as tools/<name>.py inside build/<target>/, while
// the package spells it tools/<name>.slng.py. Not an inconsistency: a dot is
// not legal in a Python module name, so the emitted project could not import
// the package's spelling. And the infix's job is to answer "may I edit this
// file", which is a question about a package a person reads rather than about a
// directory the compiler rewrites on every run.
