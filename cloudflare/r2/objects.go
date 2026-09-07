package r2

import (
	r2js "github.com/syumai/workers-go/exp/cloudflare/r2"
)

// Objects represents Cloudflare R2 objects.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L1121
type Objects struct {
	Objects   []*Object
	Truncated bool
	// Cursor indicates next cursor of Objects.
	//   - This becomes empty string if cursor doesn't exist.
	Cursor            string
	DelimitedPrefixes []string
}

// toObjects converts an r2js.R2Objects to *Objects.
func toObjects(v r2js.R2Objects) *Objects {
	objects := make([]*Object, len(v.Objects))
	for i, o := range v.Objects {
		objects[i] = toObject(o, nil)
	}
	return &Objects{
		Objects:           objects,
		Truncated:         v.Truncated,
		Cursor:            v.Cursor,
		DelimitedPrefixes: v.DelimitedPrefixes,
	}
}
