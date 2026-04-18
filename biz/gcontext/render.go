package gcontext

// ResponseRender 返回数据格式化
type ResponseRender interface {
	SetCode(int)
	SetMsg(string)
	SetData(any)
	SetDataWithFormat(any)
	SetRequestID(string)
}

func NewResponseRender() ResponseRender {
	return &responseRender{}
}

type responseRender struct {
	Code      int    `json:"code"`
	RequestID string `json:"requestID"`
	Msg       string `json:"msg"`
	Data      any    `json:"data"`
}

func (r *responseRender) SetCode(code int) {
	r.Code = code
}

func (r *responseRender) SetRequestID(requestID string) {
	r.RequestID = requestID
}

func (r *responseRender) SetMsg(msg string) {
	r.Msg = msg
}
func (r *responseRender) SetData(data any) {
	r.Data = data
}

func (r *responseRender) SetDataWithFormat(data any) {
	ResponseFormat(data)
	r.Data = data
}
