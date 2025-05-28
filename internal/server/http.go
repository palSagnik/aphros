package server

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

type httpServer struct {
	Log *Log
}

// Produce: Request Object
// A produce request contains the record that the caller of our API wants appended to the log.
type ProduceRequest struct {
	Record Record `json:"record"`
}

// Produce: Response Object
// Produce Response tells the caller what offset the log stored the records under.
type ProduceResponse struct {
	Offset uint64 `json:"offset"`
}

// Consume: Request Object
// Consume Request specifies which records the caller of the API wants to read
type ConsumeRequest struct {
	Offset uint64 `json:"offset"`
}

// Consume: Response Object
// Consume Response sends back the record to the caller
type ConsumeResponse struct {
	Record Record `json:"record"`
}

// NewHTTPServer creates and returns a new HTTP server instance configured with the specified address.
// It sets up a router with handlers for produce and consume endpoints.
// Returns:
//   - *http.Server - A configured HTTP server instance ready to be started
func NewHTTPServer(addr string) *http.Server {

	httpServer := newHTTPServer()
	r := mux.NewRouter()

	// handlers
	r.HandleFunc("/", httpServer.handleProduce).Methods("POST")
	r.HandleFunc("/", httpServer.handleConsume).Methods("GET")

	return &http.Server{
		Addr:    addr,
		Handler: r,
	}
}

// newHTTPServer creates and returns a new instance of httpServer with an initialized Log.
// The Log is created using NewLog() and is used to store and manage log records.
func newHTTPServer() *httpServer {
	return &httpServer{
		Log: NewLog(),
	}
}


// handleProduce handles HTTP requests to produce/append records to the log.
// It expects a JSON payload containing a ProduceRequest with a Record field.
// On successful append, it returns a ProduceResponse with the offset where
// the record was stored. Returns HTTP 400 for invalid JSON payload or
// HTTP 500 for internal server errors during log append or response encoding.
func (s *httpServer) handleProduce(w http.ResponseWriter, r *http.Request) {
	var request ProduceRequest

	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Append the record from the request
	offset, err := s.Log.Append(request.Record)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := ProduceResponse{Offset: offset}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}


// handleConsume handles HTTP requests to consume/read records from the log.
// It expects a JSON request body containing an offset.
// The response includes the record at the specified offset if found.
//
// The handler:
// 1. Decodes the JSON request containing the offset
// 2. Attempts to read the record at that offset from the log
// 3. Returns the record in a JSON response if found
//
// Returns:
// - 400 Bad Request: If request body is invalid
// - 404 Not Found: If no record exists at specified offset
// - 500 Internal Server Error: If there's an error reading from log or encoding response
func (s *httpServer) handleConsume(w http.ResponseWriter, r *http.Request) {
	var request ConsumeRequest

	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Read the record
	record, err := s.Log.Read(request.Offset)
	if err == ErrOffsetNotFound {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := ConsumeResponse{Record: record}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

	
		