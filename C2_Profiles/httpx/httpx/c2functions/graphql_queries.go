package c2functions

const GetPayloads = `# @genqlient
  query GetPayloadsQuery {
      payload(where: {build_phase: {_eq: "success"}, deleted: {_eq: false}, c2profileparametersinstances: {c2profile: {name: {_eq: "httpx"}}}}) {
		c2profileparametersinstances(where: {c2profile: {name: {_eq: "httpx"}}}) {
		  value
		  c2profileparameter {
			name
		  }
		}
	  }
  }
`
