package c2functions

/*
GraphQL queries here are used to generate Go code and placed in `generated.go`.
`genqlient.yaml` identifies that these are the files to process and where the resulting Go code should go.
`schema.graphql` is generated from Mythic Scripting (can be done via Jupyter container) and function to generate this is identified in Mythic's changelog docs.
`go run github.com/Khan/genqlient` will re-generate this data
*/

const GetPayloads = `# @genqlient
  query GetPayloadsQuery {
      payload(order_by: {id: asc}, where: {build_phase: {_eq: "success"}, deleted: {_eq: false}, c2profileparametersinstances: {c2profile: {name: {_eq: "httpx"}}}}) {
		payloadtype {
			name
		}
		description
		filemetum {
			filename_utf8
		}
		c2profileparametersinstances(where: {c2profile: {name: {_eq: "httpx"}}}) {
		  value
		  c2profileparameter {
			name
		  }
		}
	  }
  }
`
