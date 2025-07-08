package visualizer

// func TestUpdates(t *testing.T) {
// 	sel := &Selection{
// 		MachId: "cook",
// 		States: am.S{"Exception", "StoryCookingStarted", "StoryJoke", "Ready", "Requesting", "StoryStartAgain", "Start", "BaseDBStarting", "CheckStories"},
// 		// Active: am.S{"StoryJoke"},
// 		Active: am.S{"Ready", "StoryStartAgain", "Start", "CheckStories"},
// 	}
//
// 	path := "testdata/sample.svg"
// 	file, err := os.Open(path)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	defer file.Close()
//
// 	dom, err := goquery.NewDocumentFromReader(file)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
//
// 	err = UpdateCache(context.Background(), path, dom, sel)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// }
