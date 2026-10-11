package app

import "testing"

// TestCollections: personal and shared collections, who sees and changes them, and the
// documents list filtered to one.
func TestCollections(t *testing.T) {
	e := newEnv(t)
	c := e.c
	type coll struct {
		ID            string   `json:"id"`
		DocumentCount int      `json:"document_count"`
		Covers        []string `json:"covers"`
		CanEdit       bool     `json:"can_edit"`
	}
	type count struct {
		Count int `json:"count"`
	}
	rc := c.upload(e.family, "rc.pdf", minimalPDF("Registration certificate KA01AB1234"))
	ins := c.upload(e.family, "insurance.pdf", minimalPDF("Motor insurance policy KA01AB1234"))

	c.do("POST", "/api/v1/collections", map[string]any{"name": " "}, 422, nil)
	c.do("POST", "/api/v1/collections", map[string]any{"name": "Car", "color": "plaid"}, 422, nil)
	var mine coll
	c.do("POST", "/api/v1/collections", map[string]any{"name": "Car papers", "description": "RC, insurance, PUC", "color": "teal"}, 201, &mine)
	var n count
	c.do("POST", "/api/v1/collections/"+mine.ID+"/documents", map[string]any{"ids": []string{rc.ID, ins.ID, "00000000-0000-0000-0000-000000000001"}}, 200, &n)
	if n.Count != 2 {
		t.Fatalf("added %d, want 2 (unknown ids are skipped)", n.Count)
	}
	c.do("POST", "/api/v1/collections/"+mine.ID+"/documents", map[string]any{"ids": []string{rc.ID}}, 200, &n)
	if n.Count != 0 {
		t.Fatalf("added again: %d", n.Count)
	}
	c.do("GET", "/api/v1/collections/"+mine.ID, nil, 200, &mine)
	if mine.DocumentCount != 2 || len(mine.Covers) != 2 || !mine.CanEdit {
		t.Fatalf("collection: %+v", mine)
	}
	var docs jsonList[docDTO]
	c.do("GET", "/api/v1/documents?collection_id="+mine.ID, nil, 200, &docs)
	if len(docs.Items) != 2 {
		t.Fatalf("documents in collection: %d", len(docs.Items))
	}
	var holding jsonList[coll]
	c.do("GET", "/api/v1/collections?document_id="+rc.ID, nil, 200, &holding)
	if len(holding.Items) != 1 {
		t.Fatalf("collections holding the RC: %+v", holding.Items)
	}

	// Another person: a personal collection is invisible, even as a filter.
	c.do("POST", "/api/v1/admin/users", map[string]any{"email": "kid@example.com", "display_name": "Kid", "password": "another-long-pw"}, 201, nil)
	k := newClient(t, e.srv.URL)
	k.do("POST", "/api/v1/auth/login", map[string]any{"email": "kid@example.com", "password": "another-long-pw"}, 200, nil)
	k.do("GET", "/api/v1/collections/"+mine.ID, nil, 404, nil)
	k.do("GET", "/api/v1/documents?collection_id="+mine.ID, nil, 404, nil)

	// Shared with the family space: its viewers see it but can't change it.
	var shared coll
	c.do("POST", "/api/v1/collections", map[string]any{"name": "House", "space_id": e.family}, 201, &shared)
	c.do("POST", "/api/v1/collections/"+shared.ID+"/documents", map[string]any{"ids": []string{rc.ID}}, 200, nil)
	var dir struct{ Items []struct{ ID, Email string } }
	c.do("GET", "/api/v1/users/directory", nil, 200, &dir)
	for _, d := range dir.Items {
		if d.Email == "kid@example.com" {
			c.do("PUT", "/api/v1/spaces/"+e.family+"/members/"+d.ID, map[string]any{"role": "viewer"}, 204, nil)
		}
	}
	var kids jsonList[coll]
	k.do("GET", "/api/v1/collections", nil, 200, &kids)
	if len(kids.Items) != 1 || kids.Items[0].ID != shared.ID || kids.Items[0].CanEdit || kids.Items[0].DocumentCount != 1 {
		t.Fatalf("viewer's collections: %+v", kids.Items)
	}
	k.do("POST", "/api/v1/collections/"+shared.ID+"/documents", map[string]any{"ids": []string{ins.ID}}, 403, nil)
	k.do("PATCH", "/api/v1/collections/"+shared.ID, map[string]any{"name": "Mine now"}, 403, nil)

	// Removing takes documents out; deleting the collection keeps them.
	c.do("POST", "/api/v1/collections/"+mine.ID+"/documents/remove", map[string]any{"ids": []string{rc.ID}}, 200, &n)
	if n.Count != 1 {
		t.Fatalf("removed %d", n.Count)
	}
	c.do("PATCH", "/api/v1/collections/"+mine.ID, map[string]any{"name": "Car (old)", "color": "amber"}, 200, &mine)
	c.do("DELETE", "/api/v1/collections/"+mine.ID, nil, 204, nil)
	c.do("GET", "/api/v1/collections/"+mine.ID, nil, 404, nil)
	c.do("GET", "/api/v1/documents/"+rc.ID, nil, 200, nil)
}
