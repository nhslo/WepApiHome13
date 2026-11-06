package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	_ "github.com/jackc/pgx/v5/stdlib"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Book struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	Year        int    `json:"year"`
	IsAvailable bool   `json:"is_available"`
}
type Reader struct {
	ID       int    `json:"id"`
	FullName string `json:"full_name"`
	Email    string `json:"email"`
}
type Loan struct {
	ID       int       `json:"id"`
	BookID   int       `json:"book_id"`
	ReaderID int       `json:"reader_id"`
	Title    string    `json:"title"`
	Reader   string    `json:"reader"`
	LoanDate time.Time `json:"loan_date"`
}

var database *sql.DB

func send(responseWriter http.ResponseWriter, status int, value any) {
	responseWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
	responseWriter.WriteHeader(status)
	json.NewEncoder(responseWriter).Encode(value)
}

func fail(responseWriter http.ResponseWriter, operationError error) {
	log.Println(operationError)
	http.Error(responseWriter, "Database error", 500)
}

func idFrom(responseWriter http.ResponseWriter, request *http.Request) (int, bool) {
	id, operationError := strconv.Atoi(request.PathValue("id"))
	if operationError != nil || id <= 0 {
		http.Error(responseWriter, "Invalid ID", 400)
		return 0, false
	}
	return id, true
}

func books(responseWriter http.ResponseWriter, request *http.Request) {
	rows, operationError := database.Query("SELECT id,title,author,year,is_available FROM books ORDER BY id")
	if operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	defer rows.Close()
	items := []Book{}
	for rows.Next() {
		var book Book
		if operationError = rows.Scan(&book.ID, &book.Title, &book.Author, &book.Year, &book.IsAvailable); operationError != nil {
			fail(responseWriter, operationError)
			return
		}
		items = append(items, book)
	}
	if operationError = rows.Err(); operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	send(responseWriter, 200, items)
}

func getBook(responseWriter http.ResponseWriter, request *http.Request) {
	id, success := idFrom(responseWriter, request)
	if !success {
		return
	}
	var book Book
	operationError := database.QueryRow("SELECT id,title,author,year,is_available FROM books WHERE id=$1", id).Scan(&book.ID, &book.Title, &book.Author, &book.Year, &book.IsAvailable)
	if errors.Is(operationError, sql.ErrNoRows) {
		http.NotFound(responseWriter, request)
		return
	}
	if operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	send(responseWriter, 200, book)
}

func validBook(book Book) bool {
	return strings.TrimSpace(book.Title) != "" && strings.TrimSpace(book.Author) != "" && book.Year > 0
}

func createBook(responseWriter http.ResponseWriter, request *http.Request) {
	var book Book
	if json.NewDecoder(request.Body).Decode(&book) != nil || !validBook(book) {
		http.Error(responseWriter, "Invalid book", 400)
		return
	}
	book.IsAvailable = true
	operationError := database.QueryRow("INSERT INTO books(title,author,year) VALUES($1,$2,$3) RETURNING id", book.Title, book.Author, book.Year).Scan(&book.ID)
	if operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	send(responseWriter, 201, book)
}

func updateBook(responseWriter http.ResponseWriter, request *http.Request) {
	id, success := idFrom(responseWriter, request)
	if !success {
		return
	}
	var book Book
	if json.NewDecoder(request.Body).Decode(&book) != nil || !validBook(book) {
		http.Error(responseWriter, "Invalid book", 400)
		return
	}
	result, operationError := database.Exec("UPDATE books SET title=$1,author=$2,year=$3 WHERE id=$4", book.Title, book.Author, book.Year, id)
	if operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	numberValue, _ := result.RowsAffected()
	if numberValue == 0 {
		http.NotFound(responseWriter, request)
		return
	}
	getBook(responseWriter, request)
}

func deleteBook(responseWriter http.ResponseWriter, request *http.Request) {
	id, success := idFrom(responseWriter, request)
	if !success {
		return
	}
	var hasLoans bool
	if operationError := database.QueryRow("SELECT EXISTS(SELECT 1 FROM loans WHERE book_id=$1)", id).Scan(&hasLoans); operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	if hasLoans {
		http.Error(responseWriter, "Book has loan history", 409)
		return
	}
	result, operationError := database.Exec("DELETE FROM books WHERE id=$1", id)
	if operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	numberValue, _ := result.RowsAffected()
	if numberValue == 0 {
		http.NotFound(responseWriter, request)
		return
	}
	responseWriter.WriteHeader(204)
}

func readers(responseWriter http.ResponseWriter, request *http.Request) {
	rows, operationError := database.Query("SELECT id,full_name,email FROM readers ORDER BY id")
	if operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	defer rows.Close()
	items := []Reader{}
	for rows.Next() {
		var reader Reader
		if operationError = rows.Scan(&reader.ID, &reader.FullName, &reader.Email); operationError != nil {
			fail(responseWriter, operationError)
			return
		}
		items = append(items, reader)
	}
	if operationError = rows.Err(); operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	send(responseWriter, 200, items)
}

func createReader(responseWriter http.ResponseWriter, request *http.Request) {
	var reader Reader
	if json.NewDecoder(request.Body).Decode(&reader) != nil || strings.TrimSpace(reader.FullName) == "" || !strings.Contains(reader.Email, "@") {
		http.Error(responseWriter, "Invalid reader", 400)
		return
	}
	operationError := database.QueryRow("INSERT INTO readers(full_name,email) VALUES($1,$2) RETURNING id", reader.FullName, reader.Email).Scan(&reader.ID)
	if operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	send(responseWriter, 201, reader)
}

// В транзакции проверяем книгу и читателя, затем оформляем выдачу.
func createLoan(responseWriter http.ResponseWriter, request *http.Request) {
	var input struct {
		BookID   int `json:"book_id"`
		ReaderID int `json:"reader_id"`
	}
	if json.NewDecoder(request.Body).Decode(&input) != nil || input.BookID <= 0 || input.ReaderID <= 0 {
		http.Error(responseWriter, "Invalid loan", 400)
		return
	}
	transaction, operationError := database.Begin()
	if operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	defer transaction.Rollback()
	var available bool
	operationError = transaction.QueryRow("SELECT is_available FROM books WHERE id=$1 FOR UPDATE", input.BookID).Scan(&available)
	if errors.Is(operationError, sql.ErrNoRows) {
		http.Error(responseWriter, "Book not found", 404)
		return
	}
	if operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	if !available {
		http.Error(responseWriter, "Book unavailable", 409)
		return
	}
	var exists bool
	operationError = transaction.QueryRow("SELECT EXISTS(SELECT 1 FROM readers WHERE id=$1)", input.ReaderID).Scan(&exists)
	if operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	if !exists {
		http.Error(responseWriter, "Reader not found", 404)
		return
	}
	var id int
	operationError = transaction.QueryRow("INSERT INTO loans(book_id,reader_id) VALUES($1,$2) RETURNING id", input.BookID, input.ReaderID).Scan(&id)
	if operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	if _, operationError = transaction.Exec("UPDATE books SET is_available=false WHERE id=$1", input.BookID); operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	if operationError = transaction.Commit(); operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	send(responseWriter, 201, map[string]int{"id": id, "book_id": input.BookID, "reader_id": input.ReaderID})
}

// Отмечаем возврат и снова делаем книгу доступной в одной транзакции.
func returnLoan(responseWriter http.ResponseWriter, request *http.Request) {
	id, success := idFrom(responseWriter, request)
	if !success {
		return
	}
	transaction, operationError := database.Begin()
	if operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	defer transaction.Rollback()
	var bookID int
	var returned sql.NullTime
	operationError = transaction.QueryRow("SELECT book_id,return_date FROM loans WHERE id=$1 FOR UPDATE", id).Scan(&bookID, &returned)
	if errors.Is(operationError, sql.ErrNoRows) {
		http.NotFound(responseWriter, request)
		return
	}
	if operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	if returned.Valid {
		http.Error(responseWriter, "Already returned", 409)
		return
	}
	if _, operationError = transaction.Exec("UPDATE loans SET return_date=CURRENT_TIMESTAMP WHERE id=$1", id); operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	if _, operationError = transaction.Exec("UPDATE books SET is_available=true WHERE id=$1", bookID); operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	if operationError = transaction.Commit(); operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	send(responseWriter, 200, map[string]string{"message": "returned"})
}

func active(responseWriter http.ResponseWriter, request *http.Request) {
	rows, operationError := database.Query("SELECT l.id,l.book_id,l.reader_id,b.title,r.full_name,l.loan_date FROM loans l JOIN books b ON b.id=l.book_id JOIN readers r ON r.id=l.reader_id WHERE l.return_date IS NULL ORDER BY l.id")
	if operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	defer rows.Close()
	items := []Loan{}
	for rows.Next() {
		var loan Loan
		if operationError = rows.Scan(&loan.ID, &loan.BookID, &loan.ReaderID, &loan.Title, &loan.Reader, &loan.LoanDate); operationError != nil {
			fail(responseWriter, operationError)
			return
		}
		items = append(items, loan)
	}
	if operationError = rows.Err(); operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	send(responseWriter, 200, items)
}

func stats(responseWriter http.ResponseWriter, request *http.Request) {
	var books, available, readers, loans int
	operationError := database.QueryRow("SELECT (SELECT COUNT(*) FROM books),(SELECT COUNT(*) FROM books WHERE is_available),(SELECT COUNT(*) FROM readers),(SELECT COUNT(*) FROM loans WHERE return_date IS NULL)").Scan(&books, &available, &readers, &loans)
	if operationError != nil {
		fail(responseWriter, operationError)
		return
	}
	send(responseWriter, 200, map[string]int{"books": books, "available": available, "readers": readers, "active_loans": loans})
}

func main() {
	connectionString := os.Getenv("DATABASE_URL")
	if connectionString == "" {
		log.Fatal("Set DATABASE_URL")
	}
	var operationError error
	database, operationError = sql.Open("pgx", connectionString)
	if operationError != nil {
		log.Fatal(operationError)
	}
	defer database.Close()
	if operationError = database.Ping(); operationError != nil {
		log.Fatal(operationError)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /books", books)
	mux.HandleFunc("GET /books/{id}", getBook)
	mux.HandleFunc("POST /books", createBook)
	mux.HandleFunc("PUT /books/{id}", updateBook)
	mux.HandleFunc("DELETE /books/{id}", deleteBook)
	mux.HandleFunc("GET /readers", readers)
	mux.HandleFunc("POST /readers", createReader)
	mux.HandleFunc("POST /loans", createLoan)
	mux.HandleFunc("PUT /loans/{id}/return", returnLoan)
	mux.HandleFunc("GET /loans/active", active)
	mux.HandleFunc("GET /statistics", stats)
	log.Println("http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
