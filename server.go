package main

import (
	"cmp"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
)

var datasetPath = "dataset.xml"

type UserXML struct {
	ID        int    `xml:"id"`
	Age       int    `xml:"age"`
	FirstName string `xml:"first_name"`
	LastName  string `xml:"last_name"`
	Gender    string `xml:"gender"`
	About     string `xml:"about"`
}

type UsersXML struct {
	UserXML []UserXML `xml:"row"`
}

func ReadUsers(path string) ([]User, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	decoderXML := xml.NewDecoder(file)

	users := make([]User, 0)

	for {
		tok, err := decoderXML.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch tp := tok.(type) {
		case xml.StartElement:
			if tp.Name.Local == "row" {
				var u UserXML
				err := decoderXML.DecodeElement(&u, &tp)
				if err != nil {
					return nil, err
				}
				users = append(users, convertUser(u))
			}
		}
	}
	return users, nil
}

func convertUser(u UserXML) User {
	return User{
		ID:     u.ID,
		Name:   strings.TrimSpace(u.FirstName + " " + u.LastName),
		Age:    u.Age,
		About:  u.About,
		Gender: u.Gender,
	}
}

// Если в тестах будет чувствителен к регистру, то сделать все в нижний регистр
// Поиск людей, которые именю в Name или About
func filterUsers(users []User, query string) []User {
	if query == "" {
		return users
	}

	queryUsers := []User{}
	query = strings.ToLower(query)
	for _, u := range users {
		if strings.Contains(strings.ToLower(u.Name), query) || strings.Contains(strings.ToLower(u.About), query) {
			queryUsers = append(queryUsers, u)
		}
	}
	return queryUsers
}

func sortUsers(users []User, sortBy string, order, offset, limit string) ([]User, error) {
	if order == "" {
		order = "0"
	}
	if offset == "" {
		offset = "0"
	}
	if limit == "" {
		limit = strconv.Itoa(len(users))
	}
	orderInt, err := strconv.Atoi(order)
	if err != nil {
		return nil, err
	}
	offsetInt, err := strconv.Atoi(offset)
	if err != nil {
		return nil, err
	}
	limitInt, err := strconv.Atoi(limit)
	if err != nil {
		return nil, err
	}
	if offsetInt < 0 || limitInt < 0 {
		return nil, fmt.Errorf("negative offset or limit")
	}
	if orderInt != OrderByAsc && orderInt != OrderByDesc && orderInt != OrderByAsIs {
		return nil, fmt.Errorf("invalid order_by: %d", orderInt)
	}
	var compare func(a, b User) int
	switch sortBy {
	case "", "name", "Name":
		compare = func(a, b User) int { return cmp.Compare(a.Name, b.Name) }
	case "id", "Id":
		compare = func(a, b User) int { return cmp.Compare(a.ID, b.ID) }
	case "age", "Age":
		compare = func(a, b User) int { return cmp.Compare(a.Age, b.Age) }
	default:
		return nil, fmt.Errorf(ErrorBadOrderField)
	}
	if orderInt == OrderByAsc {
		slices.SortStableFunc(users, compare)
	}
	if orderInt == OrderByDesc {
		slices.SortStableFunc(users, func(a, b User) int { return -compare(a, b) })
	}
	if offsetInt >= len(users) {
		return []User{}, nil
	}
	users = users[offsetInt:]
	if limitInt < len(users) {
		users = users[:limitInt]
	}
	return users, nil
}

func writeSearchError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(SearchErrorResponse{Error: message}); err != nil {
		log.Printf("encode error: %v", err)
	}
}

func SearchServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeSearchError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	params := r.URL.Query()
	users, err := ReadUsers(datasetPath)
	if err != nil {
		writeSearchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	users = filterUsers(users, params.Get("query"))
	users, err = sortUsers(users, params.Get("order_field"), params.Get("order_by"), params.Get("offset"), params.Get("limit"))
	if err != nil {
		writeSearchError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(users); err != nil {
		log.Printf("encode: %v", err)
	}
}
