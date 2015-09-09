package main

func deployAgent(args []string) {
	c := Client{
		URL:   args[0],
		Token: args[1],
	}
	_, err := c.registerUnit(args[2], nil)
	if err != nil {
		panic(err)
	}
}
