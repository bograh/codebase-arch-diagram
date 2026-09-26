package api

import (
	"example.com/dep/strutil"
	"example.com/layered/internal/db"
)

type Server struct{ conn int }

func New() *Server { return &Server{conn: db.Conn + len(strutil.Upper("x"))} }

func helper() {}
