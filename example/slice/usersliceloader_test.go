package slice_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tribunadigital/dataloaden/example"
	"github.com/tribunadigital/dataloaden/example/slice"
)

func TestUserLoader(t *testing.T) {
	var fetches [][]string
	var mu sync.Mutex

	dl := slice.NewUserSliceLoader(slice.UserSliceLoaderConfig{
		Fetch: func(keys []string) (users [][]example.User, errors []error) {
			mu.Lock()
			fetches = append(fetches, keys)
			mu.Unlock()

			users = make([][]example.User, len(keys))
			errors = make([]error, len(keys))

			for i, key := range keys {
				if strings.HasSuffix(key, "0") { // anything ending in zero is bad
					errors[i] = fmt.Errorf("users not found")
				} else {
					users[i] = []example.User{
						{ID: key, Name: "user " + key},
						{ID: key, Name: "user " + key},
					}
				}
			}
			return users, errors
		},
		Wait:     10 * time.Millisecond,
		MaxBatch: 5,
		Cache:    slice.NewUserSliceLoaderGoCache(slice.UserSliceLoaderGoCacheConfig{}),
	})

	t.Run("fetch concurrent data", func(t *testing.T) {
		t.Run("load user successfully", func(t *testing.T) {
			t.Parallel()
			u, err := dl.Load("1")
			require.NoError(t, err)
			require.Equal(t, u[0].ID, "1")
			require.Equal(t, u[1].ID, "1")
		})

		t.Run("load failed user", func(t *testing.T) {
			t.Parallel()
			u, err := dl.Load("10")
			require.Error(t, err)
			require.Nil(t, u)
		})

		t.Run("load many users", func(t *testing.T) {
			t.Parallel()
			u, err := dl.LoadAll([]string{"2", "10", "20", "4"})
			require.Equal(t, u[0][0].Name, "user 2")
			require.Error(t, err[1])
			require.Error(t, err[2])
			require.Equal(t, u[3][0].Name, "user 4")
		})

		t.Run("load thunk", func(t *testing.T) {
			t.Parallel()
			thunk1 := dl.LoadThunk("5")
			thunk2 := dl.LoadThunk("50")

			u1, err1 := thunk1()
			require.NoError(t, err1)
			require.Equal(t, "user 5", u1[0].Name)

			u2, err2 := thunk2()
			require.Error(t, err2)
			require.Nil(t, u2)
		})
	})

	t.Run("it sent two batches", func(t *testing.T) {
		mu.Lock()
		defer mu.Unlock()

		require.Len(t, fetches, 2)
		assert.Len(t, fetches[0], 5)
		assert.Len(t, fetches[1], 3)
	})

	t.Run("fetch more", func(t *testing.T) {

		t.Run("previously cached", func(t *testing.T) {
			t.Parallel()
			u, err := dl.Load("1")
			require.NoError(t, err)
			require.Equal(t, u[0].ID, "1")
		})

		t.Run("load many users", func(t *testing.T) {
			t.Parallel()
			u, err := dl.LoadAll([]string{"2", "4"})
			require.NoError(t, err[0])
			require.NoError(t, err[1])
			require.Equal(t, u[0][0].Name, "user 2")
			require.Equal(t, u[1][0].Name, "user 4")
		})
	})

	t.Run("no round trips", func(t *testing.T) {
		mu.Lock()
		defer mu.Unlock()

		require.Len(t, fetches, 2)
	})

	t.Run("fetch partial", func(t *testing.T) {
		t.Run("errors not in cache cache value", func(t *testing.T) {
			t.Parallel()
			u, err := dl.Load("20")
			require.Nil(t, u)
			require.Error(t, err)
		})

		t.Run("load all", func(t *testing.T) {
			t.Parallel()
			u, err := dl.LoadAll([]string{"1", "4", "10", "9", "5"})
			require.Equal(t, u[0][0].ID, "1")
			require.Equal(t, u[1][0].ID, "4")
			require.Error(t, err[2])
			require.Equal(t, u[3][0].ID, "9")
			require.Equal(t, u[4][0].ID, "5")
		})
	})

	t.Run("one partial trip", func(t *testing.T) {
		mu.Lock()
		defer mu.Unlock()

		require.Len(t, fetches, 3)
		require.Len(t, fetches[2], 3) // E1 U9 E2 in some random order
	})

	t.Run("primed reads dont hit the fetcher", func(t *testing.T) {
		dl.Prime("99", []example.User{
			{ID: "U99", Name: "Primed user"},
			{ID: "U99", Name: "Primed user"},
		})
		u, err := dl.Load("99")
		require.NoError(t, err)
		require.Equal(t, "Primed user", u[0].Name)

		require.Len(t, fetches, 3)
	})

	t.Run("priming in a loop is safe", func(t *testing.T) {
		users := [][]example.User{
			{{ID: "123", Name: "Alpha"}, {ID: "123", Name: "Alpha"}},
			{{ID: "124", Name: "Omega"}, {ID: "124", Name: "Omega"}},
		}
		for _, user := range users {
			id := user[0].ID
			dl.Prime(id, user)
		}

		u, err := dl.Load("123")
		require.NoError(t, err)
		require.Equal(t, "Alpha", u[0].Name)

		u, err = dl.Load("124")
		require.NoError(t, err)
		require.Equal(t, "Omega", u[0].Name)

		require.Len(t, fetches, 3)
	})

	t.Run("cleared results will go back to the fetcher", func(t *testing.T) {
		dl.Clear("99")
		u, err := dl.Load("99")
		require.NoError(t, err)
		require.Equal(t, "user 99", u[0].Name)

		require.Len(t, fetches, 4)
	})

	t.Run("load all thunk", func(t *testing.T) {
		thunk1 := dl.LoadAllThunk([]string{"5", "6"})
		thunk2 := dl.LoadAllThunk([]string{"6", "60"})

		users1, err1 := thunk1()

		require.NoError(t, err1[0])
		require.NoError(t, err1[1])
		require.Equal(t, "user 5", users1[0][0].Name)
		require.Equal(t, "user 6", users1[1][0].Name)

		users2, err2 := thunk2()

		require.NoError(t, err2[0])
		require.Error(t, err2[1])
		require.Equal(t, "user 6", users2[0][0].Name)
	})
}
