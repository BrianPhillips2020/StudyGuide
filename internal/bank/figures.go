package bank

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/ledongthuc/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"studyguide/internal/store"
)

func init() {
	// Keep pdfcpu from writing a config directory under the user's profile.
	model.ConfigPath = "disable"
}

// imageRef identifies an image XObject drawn on a page, e.g. page 4 "Image35".
func imageRef(page int, name string) string { return fmt.Sprintf("%d/%s", page, name) }

// placedImage is an image draw with the y of its top edge in page space.
type placedImage struct {
	ref string
	top float64
}

// pageImages finds every image drawn on a page and where it is placed, by
// tracking the current transformation matrix through the content stream.
// An image is drawn into the unit square mapped by the CTM, so its top edge
// sits at f+d.
func pageImages(p pdf.Page, pageNum int) (out []placedImage, err error) {
	contents := p.V.Key("Contents")
	if contents.IsNull() {
		return nil, nil
	}
	xobjects := p.Resources().Key("XObject")

	// The library panics on malformed streams; report that as an error.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("page %d: reading drawing commands: %v", pageNum, r)
		}
	}()

	type mat [6]float64 // a b c d e f
	mul := func(m, n mat) mat {
		return mat{
			m[0]*n[0] + m[1]*n[2], m[0]*n[1] + m[1]*n[3],
			m[2]*n[0] + m[3]*n[2], m[2]*n[1] + m[3]*n[3],
			m[4]*n[0] + m[5]*n[2] + n[4], m[4]*n[1] + m[5]*n[3] + n[5],
		}
	}
	ctm := mat{1, 0, 0, 1, 0, 0}
	var saved []mat

	pdf.Interpret(contents, func(stk *pdf.Stack, op string) {
		args := make([]pdf.Value, stk.Len())
		for i := len(args) - 1; i >= 0; i-- {
			args[i] = stk.Pop()
		}
		switch op {
		case "q":
			saved = append(saved, ctm)
		case "Q":
			if n := len(saved); n > 0 {
				ctm, saved = saved[n-1], saved[:n-1]
			}
		case "cm":
			if len(args) == 6 {
				var m mat
				for i := range m {
					m[i] = args[i].Float64()
				}
				ctm = mul(m, ctm)
			}
		case "Do":
			if len(args) == 1 && xobjects.Key(args[0].Name()).Key("Subtype").Name() == "Image" {
				out = append(out, placedImage{ref: imageRef(pageNum, args[0].Name()), top: ctm[5] + ctm[3]})
			}
		}
	})
	return out, nil
}

// withImages merges image draws into the text rows of one page, in reading
// order (top of the page first), as lines whose image field is set.
func withImages(rows []line, imgs []placedImage) []line {
	if len(imgs) == 0 {
		return rows
	}
	sort.SliceStable(imgs, func(i, j int) bool { return imgs[i].top > imgs[j].top })
	out := make([]line, 0, len(rows)+len(imgs))
	for _, r := range rows {
		for len(imgs) > 0 && imgs[0].top >= r.y {
			out = append(out, line{page: r.page, image: imgs[0].ref, y: imgs[0].top})
			imgs = imgs[1:]
		}
		out = append(out, r)
	}
	for _, im := range imgs {
		out = append(out, line{page: rows[0].page, image: im.ref, y: im.top})
	}
	return out
}

// extractImages returns the bytes of every image in the PDF, keyed by
// imageRef. JPEGs come through unchanged; other encodings are converted to
// PNG by pdfcpu.
func extractImages(path string) (map[string]*store.Figure, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	pages, err := api.ExtractImagesRaw(context.Background(), f, nil, conf)
	if err != nil {
		return nil, fmt.Errorf("extract images: %w", err)
	}

	out := map[string]*store.Figure{}
	for _, byObj := range pages {
		for _, img := range byObj {
			mime := map[string]string{"jpg": "image/jpeg", "png": "image/png"}[img.FileType]
			if mime == "" {
				continue // e.g. TIFF, which the WebView can't show; reported as missing
			}
			data, err := io.ReadAll(img)
			if err != nil {
				return nil, fmt.Errorf("page %d image %s: %w", img.PageNr, img.Name, err)
			}
			out[imageRef(img.PageNr, img.Name)] = &store.Figure{Mime: mime, Data: data}
		}
	}
	return out, nil
}
