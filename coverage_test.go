package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func extractIDs(users []User) []int {
	ids := make([]int, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	return ids
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *SearchClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &SearchClient{URL: srv.URL, AccessToken: "test-token"}
}

func TestConvertUser(t *testing.T) {
	tests := []struct {
		name string
		in   UserXML
		want User
	}{
		{
			name: "полные данные",
			in:   UserXML{ID: 1, FirstName: "Alice", LastName: "Smith", Age: 30, Gender: "female", About: "dev"},
			want: User{ID: 1, Name: "Alice Smith", Age: 30, Gender: "female", About: "dev"},
		},
		{
			name: "пустое имя и фамилия",
			in:   UserXML{ID: 2, Age: 20},
			want: User{ID: 2, Name: "", Age: 20},
		},
		{
			name: "только имя",
			in:   UserXML{ID: 3, FirstName: "Bob"},
			want: User{ID: 3, Name: "Bob"},
		},
		{
			name: "только фамилия",
			in:   UserXML{ID: 4, LastName: "Jones"},
			want: User{ID: 4, Name: "Jones"},
		},
		{
			name: "нулевой ID",
			in:   UserXML{FirstName: "X", LastName: "Y"},
			want: User{Name: "X Y"},
		},
		{
			name: "кириллица",
			in:   UserXML{ID: 5, FirstName: "Иван", LastName: "Петров"},
			want: User{ID: 5, Name: "Иван Петров"},
		},
		{
			name: "пробелы по краям",
			in:   UserXML{ID: 6, FirstName: "  Alice  ", LastName: "  Smith  "},
			want: User{ID: 6, Name: "Alice     Smith"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertUser(tt.in)
			if got != tt.want {
				t.Errorf("convertUser() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReadUsers(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		users, err := ReadUsers("dataset.xml")
		if err != nil {
			t.Fatalf("ReadUsers: %v", err)
		}
		if len(users) == 0 {
			t.Fatal("ожидались пользователи")
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := ReadUsers("nonexistent.xml")
		if err == nil {
			t.Error("ожидалась ошибка")
		}
	})

	t.Run("directory instead of file", func(t *testing.T) {
		_, err := ReadUsers(".")
		if err == nil {
			t.Error("ожидалась ошибка")
		}
	})

	t.Run("empty path", func(t *testing.T) {
		_, err := ReadUsers("")
		if err == nil {
			t.Error("ожидалась ошибка")
		}
	})

	t.Run("invalid XML", func(t *testing.T) {
		tmp, err := os.CreateTemp("", "bad*.xml")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmp.Name())
		_, _ = tmp.WriteString(`<root><row><id>1</id>`)
		tmp.Close()

		_, err = ReadUsers(tmp.Name())
		if err == nil {
			t.Error("ожидалась ошибка на битом XML")
		}
	})

	t.Run("empty XML", func(t *testing.T) {
		tmp, err := os.CreateTemp("", "empty*.xml")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmp.Name())
		_, _ = tmp.WriteString(`<?xml version="1.0"?><root></root>`)
		tmp.Close()

		users, err := ReadUsers(tmp.Name())
		if err != nil {
			t.Fatalf("неожиданная ошибка: %v", err)
		}
		if len(users) != 0 {
			t.Errorf("len = %d, want 0", len(users))
		}
	})
}

func TestFilterUsers(t *testing.T) {
	users, err := ReadUsers("dataset.xml")
	if err != nil {
		t.Fatalf("ReadUsers: %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantLen int
		wantIDs []int
	}{
		{"пустой query → все", "", len(users), nil},
		{"по имени Whitley", "Whitley", 1, []int{13}},
		{"регистр WHITLEY", "WHITLEY", 1, []int{13}},
		{"регистр wHiTlEy", "wHiTlEy", 1, []int{13}},
		{"нет совпадений", "zzzznonexistent", 0, nil},
		{"спецсимволы", "!@#$%", 0, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterUsers(slices.Clone(users), tt.query)
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantIDs != nil {
				gotIDs := extractIDs(got)
				if !slices.Equal(gotIDs, tt.wantIDs) {
					t.Errorf("IDs = %v, want %v", gotIDs, tt.wantIDs)
				}
			}
		})
	}

	t.Run("не мутирует вход", func(t *testing.T) {
		original := slices.Clone(users)
		_ = filterUsers(users, "Whitley")
		if !slices.Equal(users, original) {
			t.Error("входной слайс мутирован")
		}
	})

	t.Run("пустой слайс", func(t *testing.T) {
		got := filterUsers(nil, "x")
		if len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
	})

	t.Run("каждый результат содержит query", func(t *testing.T) {
		q := strings.ToLower("veniam")
		for _, u := range filterUsers(slices.Clone(users), "veniam") {
			if !strings.Contains(strings.ToLower(u.Name), q) &&
				!strings.Contains(strings.ToLower(u.About), q) {
				t.Errorf("user %d не содержит %q", u.ID, q)
			}
		}
	})
}

// ТЕСТЫ sortUsers

func TestSortUsers(t *testing.T) {
	users, err := ReadUsers("dataset.xml")
	if err != nil {
		t.Fatalf("ReadUsers: %v", err)
	}

	tests := []struct {
		name    string
		sortBy  string
		order   string
		offset  string
		limit   string
		wantIDs []int
		wantErr bool
	}{
		{
			name:    "sort by name asc",
			sortBy:  "name",
			order:   "1",
			offset:  "0",
			limit:   "10",
			wantIDs: []int{15, 16, 19, 22, 5, 0, 2, 32, 29, 28},
		},
		{
			name:    "sort by name desc",
			sortBy:  "name",
			order:   "-1",
			offset:  "0",
			limit:   "10",
			wantIDs: []int{13, 33, 18, 26, 9, 27, 31, 4, 14, 20},
		},
		{
			name:    "sort by id asc",
			sortBy:  "id",
			order:   "1",
			offset:  "0",
			limit:   "3",
			wantIDs: []int{0, 1, 2},
		},
		{
			name:    "sort by id desc",
			sortBy:  "id",
			order:   "-1",
			offset:  "0",
			limit:   "3",
			wantIDs: []int{34, 33, 32},
		},
		{
			name:    "offset=1 limit=2 по id",
			sortBy:  "id",
			order:   "1",
			offset:  "1",
			limit:   "2",
			wantIDs: []int{1, 2},
		},
		{
			name:    "пустой sortBy → name",
			sortBy:  "",
			order:   "1",
			offset:  "0",
			limit:   "3",
			wantIDs: []int{15, 16, 19},
		},
		{
			name:    "пустой order → asc",
			sortBy:  "id",
			order:   "",
			offset:  "0",
			limit:   "3",
			wantIDs: []int{0, 1, 2},
		},
		{
			name:    "пустой offset → 0",
			sortBy:  "id",
			order:   "1",
			offset:  "",
			limit:   "3",
			wantIDs: []int{0, 1, 2},
		},
		{
			name:    "limit=0 → пусто",
			sortBy:  "id",
			order:   "1",
			offset:  "0",
			limit:   "0",
			wantIDs: []int{},
		},
		{
			name:    "order_by=0 → asc",
			sortBy:  "id",
			order:   "0",
			offset:  "0",
			limit:   "3",
			wantIDs: []int{0, 1, 2},
		},
		// Ошибки
		{name: "неизвестный sortBy", sortBy: "unknown", order: "1", offset: "0", limit: "5", wantErr: true},
		{name: "order_by=abc", sortBy: "id", order: "abc", offset: "0", limit: "5", wantErr: true},
		{name: "offset=abc", sortBy: "id", order: "1", offset: "abc", limit: "5", wantErr: true},
		{name: "limit=abc", sortBy: "id", order: "1", offset: "0", limit: "abc", wantErr: true},
		{name: "offset < 0", sortBy: "id", order: "1", offset: "-1", limit: "5", wantErr: true},
		{name: "limit < 0", sortBy: "id", order: "1", offset: "0", limit: "-5", wantErr: true},
		{name: "offset > len", sortBy: "id", order: "1", offset: "1000", limit: "5", wantIDs: []int{}},
		{name: "limit > len-offset", sortBy: "id", order: "1", offset: "0", limit: "1000", wantIDs: extractIDs(users)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sortUsers(slices.Clone(users), tt.sortBy, tt.order, tt.offset, tt.limit)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ожидалась ошибка, но её нет")
				}
				return
			}

			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}

			gotIDs := extractIDs(got)
			if !slices.Equal(gotIDs, tt.wantIDs) {
				t.Errorf("IDs = %v, want %v", gotIDs, tt.wantIDs)
			}
		})
	}

	t.Run("age asc — свойство", func(t *testing.T) {
		got, err := sortUsers(slices.Clone(users), "age", "1", "0", "10")
		if err != nil {
			t.Fatal(err)
		}
		for i := 1; i < len(got); i++ {
			if got[i-1].Age > got[i].Age {
				t.Errorf("не asc: %d > %d", got[i-1].Age, got[i].Age)
			}
		}
	})

	t.Run("age desc — свойство", func(t *testing.T) {
		got, err := sortUsers(slices.Clone(users), "age", "-1", "0", "10")
		if err != nil {
			t.Fatal(err)
		}
		for i := 1; i < len(got); i++ {
			if got[i-1].Age < got[i].Age {
				t.Errorf("не desc: %d < %d", got[i-1].Age, got[i].Age)
			}
		}
	})

	t.Run("пустой слайс", func(t *testing.T) {
		got, err := sortUsers(nil, "id", "1", "0", "10")
		if err != nil {
			t.Fatalf("неожиданная ошибка: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
	})
}

func TestSearchServer(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		query      string
		wantStatus int
		wantCount  int // -1 = не проверять
	}{
		// Успешные
		{name: "GET без параметров", method: http.MethodGet, query: "", wantStatus: http.StatusOK, wantCount: -1},
		{name: "GET query=Whitley", method: http.MethodGet, query: "query=Whitley", wantStatus: http.StatusOK, wantCount: 1},
		{name: "GET query=zzz", method: http.MethodGet, query: "query=zzzzz", wantStatus: http.StatusOK, wantCount: 0},
		{name: "GET limit=4", method: http.MethodGet, query: "order_field=age&order_by=1&limit=4", wantStatus: http.StatusOK, wantCount: 4},
		{name: "GET limit=0", method: http.MethodGet, query: "limit=0", wantStatus: http.StatusOK, wantCount: 0},
		{name: "GET offset=1 limit=2", method: http.MethodGet, query: "offset=1&limit=2", wantStatus: http.StatusOK, wantCount: 2},
		{name: "GET query+sort", method: http.MethodGet, query: "query=veniam&order_field=age&order_by=-1&limit=3", wantStatus: http.StatusOK, wantCount: 3},
		{name: "GET order_by=0", method: http.MethodGet, query: "order_by=0&limit=3", wantStatus: http.StatusOK, wantCount: 3},

		// Метод
		{name: "POST", method: http.MethodPost, query: "", wantStatus: http.StatusMethodNotAllowed},
		{name: "PUT", method: http.MethodPut, query: "", wantStatus: http.StatusMethodNotAllowed},
		{name: "DELETE", method: http.MethodDelete, query: "", wantStatus: http.StatusMethodNotAllowed},

		// Ошибки
		{name: "невалидный order_field", method: http.MethodGet, query: "order_field=noname", wantStatus: http.StatusBadRequest},
		{name: "невалидный order", method: http.MethodGet, query: "order_by=abc", wantStatus: http.StatusBadRequest},
		{name: "невалидный offset", method: http.MethodGet, query: "offset=abc", wantStatus: http.StatusBadRequest},
		{name: "невалидный limit", method: http.MethodGet, query: "limit=abc", wantStatus: http.StatusBadRequest},
		{name: "offset < 0", method: http.MethodGet, query: "offset=-1", wantStatus: http.StatusBadRequest},
		{name: "limit < 0", method: http.MethodGet, query: "limit=-1", wantStatus: http.StatusBadRequest},
		{name: "offset > len", method: http.MethodGet, query: "offset=1000", wantStatus: http.StatusOK, wantCount: 0},
		{name: "limit > len", method: http.MethodGet, query: "limit=1000", wantStatus: http.StatusOK, wantCount: 35},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/?"+tt.query, nil)
			w := httptest.NewRecorder()

			SearchServer(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body=%s", w.Code, tt.wantStatus, w.Body.String())
				return
			}

			if tt.wantCount >= 0 && w.Code == http.StatusOK {
				var users []User
				if err := json.NewDecoder(w.Body).Decode(&users); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if len(users) != tt.wantCount {
					t.Errorf("count = %d, want %d", len(users), tt.wantCount)
				}
			}
		})
	}
}

func TestSearchServer_ResponseFormat(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?limit=2", nil)
	w := httptest.NewRecorder()

	SearchServer(w, req)

	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}

	var users []User
	if err := json.NewDecoder(w.Body).Decode(&users); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("len = %d, want 2", len(users))
	}
}

func TestSearchServer_SortOrder(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?order_field=age&order_by=1&limit=10", nil)
	w := httptest.NewRecorder()
	SearchServer(w, req)

	var users []User
	_ = json.NewDecoder(w.Body).Decode(&users)
	for i := 1; i < len(users); i++ {
		if users[i-1].Age > users[i].Age {
			t.Errorf("не asc: %d > %d", users[i-1].Age, users[i].Age)
		}
	}
}

func TestSearchServer_FilterApplied(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?query=Whitley", nil)
	w := httptest.NewRecorder()
	SearchServer(w, req)

	var users []User
	_ = json.NewDecoder(w.Body).Decode(&users)
	if len(users) != 1 || users[0].ID != 13 {
		t.Errorf("got %v, want 1 user с ID 13", extractIDs(users))
	}
}

func TestSearchServer_ReadUsersError(t *testing.T) {
	oldPath := datasetPath
	datasetPath = "nonexistent.xml"
	defer func() { datasetPath = oldPath }()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	SearchServer(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestSearchServer_InvalidXML(t *testing.T) {
	tmp, err := os.CreateTemp("", "bad*.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	_, _ = tmp.WriteString(`<root><row><id>1</id>`)
	tmp.Close()

	oldPath := datasetPath
	datasetPath = tmp.Name()
	defer func() { datasetPath = oldPath }()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	SearchServer(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestFindUsers_Table(t *testing.T) {
	threeUsers := []User{{ID: 1}, {ID: 2}, {ID: 3}}
	fourUsers := []User{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}

	tests := []struct {
		name      string
		req       SearchRequest
		handler   http.HandlerFunc
		wantErr   string
		wantUsers int
		wantNext  bool
		wantLimit string
	}{
		{
			name: "success: меньше limit",
			req:  SearchRequest{Limit: 10, OrderBy: 1},
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, threeUsers)
			},
			wantUsers: 3, wantNext: false, wantLimit: "11",
		},
		{
			name: "success: ровно limit+1 → NextPage=true",
			req:  SearchRequest{Limit: 3, OrderBy: 1},
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, fourUsers)
			},
			wantUsers: 3, wantNext: true, wantLimit: "4",
		},
		{
			name: "success: пустой ответ",
			req:  SearchRequest{Limit: 10, OrderBy: 1},
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, []User{})
			},
			wantUsers: 0, wantNext: false, wantLimit: "11",
		},
		{
			name: "success: limit=25 → cap 25, +1=26",
			req:  SearchRequest{Limit: 25, OrderBy: 1},
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, []User{})
			},
			wantUsers: 0, wantLimit: "26",
		},
		{
			name: "success: limit=100 → cap 25, +1=26",
			req:  SearchRequest{Limit: 100, OrderBy: 1},
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, []User{})
			},
			wantUsers: 0, wantLimit: "26",
		},
		{
			name: "success: limit=0 → +1=1",
			req:  SearchRequest{Limit: 0},
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, []User{})
			},
			wantUsers: 0, wantLimit: "1",
		},
		{
			name: "error: limit < 0",
			req:  SearchRequest{Limit: -1},
			handler: func(w http.ResponseWriter, r *http.Request) {
				t.Error("запрос не должен был отправиться")
			},
			wantErr: "limit must be > 0",
		},
		{
			name: "error: offset < 0",
			req:  SearchRequest{Limit: 10, Offset: -1},
			handler: func(w http.ResponseWriter, r *http.Request) {
				t.Error("запрос не должен был отправиться")
			},
			wantErr: "offset must be > 0",
		},
		{
			name: "error: 401",
			req:  SearchRequest{Limit: 10},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			wantErr: "bad AccessToken",
		},
		{
			name: "error: 500",
			req:  SearchRequest{Limit: 10},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: "SearchServer fatal error",
		},
		{
			name: "error: 400 BadOrderField",
			req:  SearchRequest{Limit: 10, OrderField: "xxx"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusBadRequest, SearchErrorResponse{Error: ErrorBadOrderField})
			},
			wantErr: "OrderFeld xxx invalid",
		},
		{
			name: "error: 400 unknown",
			req:  SearchRequest{Limit: 10},
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusBadRequest, SearchErrorResponse{Error: "other"})
			},
			wantErr: "unknown bad request error",
		},
		{
			name: "error: 400 invalid JSON",
			req:  SearchRequest{Limit: 10},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("not json"))
			},
			wantErr: "cant unpack error json",
		},
		{
			name: "error: 200 invalid JSON",
			req:  SearchRequest{Limit: 10},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("not json"))
			},
			wantErr: "cant unpack result json",
		},
		{
			name: "error: 404 → cant unpack",
			req:  SearchRequest{Limit: 10},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: "cant unpack result json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotLimit, gotOrder string

			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotLimit = r.URL.Query().Get("limit")
				gotOrder = r.URL.Query().Get("order_by")
				tt.handler(w, r)
			})

			resp, err := client.FindUsers(tt.req)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ожидалась ошибка, содержащая %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("err = %q, want содержит %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if resp == nil {
				t.Fatal("resp == nil")
			}
			if len(resp.Users) != tt.wantUsers {
				t.Errorf("len(Users) = %d, want %d", len(resp.Users), tt.wantUsers)
			}
			if resp.NextPage != tt.wantNext {
				t.Errorf("NextPage = %v, want %v", resp.NextPage, tt.wantNext)
			}
			if tt.wantLimit != "" && gotLimit != tt.wantLimit {
				t.Errorf("limit = %q, want %q", gotLimit, tt.wantLimit)
			}
			if gotOrder != strconv.Itoa(tt.req.OrderBy) {
				t.Errorf("order = %q, want %q", gotOrder, strconv.Itoa(tt.req.OrderBy))
			}
		})
	}
}

func TestFindUsers_AccessToken(t *testing.T) {
	var gotToken string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("AccessToken")
		writeJSON(w, http.StatusOK, []User{})
	})

	_, err := client.FindUsers(SearchRequest{Limit: 10})
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if gotToken != "test-token" {
		t.Errorf("AccessToken = %q, want %q", gotToken, "test-token")
	}
}

func TestFindUsers_AllParams(t *testing.T) {
	var params url.Values
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		params = r.URL.Query()
		writeJSON(w, http.StatusOK, []User{})
	})

	_, err := client.FindUsers(SearchRequest{
		Limit:      10,
		Offset:     20,
		Query:      "test",
		OrderField: "age",
		OrderBy:    -1,
	})
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	checks := map[string]string{
		"limit":       "11",
		"offset":      "20",
		"query":       "test",
		"order_field": "age",
		"order_by":    "-1",
	}
	for k, want := range checks {
		if got := params.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestFindUsers_QueryWithSpaces(t *testing.T) {
	var gotQuery string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		writeJSON(w, http.StatusOK, []User{})
	})

	_, err := client.FindUsers(SearchRequest{Limit: 5, Query: "John Doe"})
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if gotQuery != "John Doe" {
		t.Errorf("query = %q, want %q", gotQuery, "John Doe")
	}
}

func TestIntegration_ClientServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(SearchServer))
	t.Cleanup(srv.Close)

	client := &SearchClient{URL: srv.URL, AccessToken: "any"}

	resp, err := client.FindUsers(SearchRequest{
		Limit:      10,
		Query:      "Whitley",
		OrderField: "name",
		OrderBy:    1,
	})
	if err != nil {
		t.Fatalf("FindUsers: %v", err)
	}
	if resp == nil {
		t.Fatal("resp == nil")
	}
	if len(resp.Users) != 1 {
		t.Fatalf("len(Users) = %d, want 1", len(resp.Users))
	}
	if resp.Users[0].ID != 13 {
		t.Errorf("ID = %d, want 13", resp.Users[0].ID)
	}
	if !strings.Contains(strings.ToLower(resp.Users[0].Name), "whitley") {
		t.Errorf("Name = %q, ожидалось 'Whitley Davidson'", resp.Users[0].Name)
	}
}
