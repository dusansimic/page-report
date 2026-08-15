package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"

	pagereportv1 "github.com/dusan/page-report/gen/pagereport/v1"
	"github.com/dusan/page-report/internal/id"
	"github.com/dusan/page-report/internal/store"
)

// rpcService implements pagereportv1connect.PageServiceHandler.
type rpcService struct {
	s *Server
}

func (r *rpcService) GetServerInfo(
	_ context.Context,
	_ *connect.Request[pagereportv1.GetServerInfoRequest],
) (*connect.Response[pagereportv1.GetServerInfoResponse], error) {
	return connect.NewResponse(&pagereportv1.GetServerInfoResponse{
		BaseUrl:       r.s.cfg.BaseURL,
		TokensUrl:     r.s.cfg.TokensURL(),
		ServerVersion: Version,
	}), nil
}

func (r *rpcService) WhoAmI(
	ctx context.Context,
	_ *connect.Request[pagereportv1.WhoAmIRequest],
) (*connect.Response[pagereportv1.WhoAmIResponse], error) {
	identity, _ := IdentityFrom(ctx)
	info, _ := TokenInfoFrom(ctx)
	return connect.NewResponse(&pagereportv1.WhoAmIResponse{
		Subject:   identity.Subject,
		Email:     identity.Email,
		Login:     identity.Login,
		TokenId:   info.TokenID,
		TokenName: info.TokenName,
	}), nil
}

func (r *rpcService) UploadPage(
	ctx context.Context,
	req *connect.Request[pagereportv1.UploadPageRequest],
) (*connect.Response[pagereportv1.UploadPageResponse], error) {
	content := req.Msg.GetContent()
	if len(content) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("content must not be empty"))
	}
	if int64(len(content)) > r.s.cfg.MaxUploadBytes {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("content exceeds max upload size of %d bytes", r.s.cfg.MaxUploadBytes))
	}
	contentType := defaultContentType
	if raw := req.Msg.GetContentType(); raw != "" {
		canonical, ok := canonicalContentType(raw)
		if !ok {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("content type %q is not allowed: use text/html or text/plain", raw))
		}
		contentType = canonical
	}
	identity, _ := IdentityFrom(ctx)
	createdBy := identity.Email
	if createdBy == "" {
		createdBy = identity.Login
	}
	if createdBy == "" {
		createdBy = identity.Subject
	}

	var pageID string
	for attempt := 0; ; attempt++ {
		newID, err := id.New()
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		err = r.s.store.CreatePage(ctx, store.Page{
			ID:           newID,
			Title:        req.Msg.GetTitle(),
			Content:      content,
			ContentType:  contentType,
			SizeBytes:    int64(len(content)),
			CreatedAt:    time.Now().UTC(),
			CreatedBy:    createdBy,
			OwnerSubject: identity.Subject,
		})
		if err == nil {
			pageID = newID
			break
		}
		if store.IsDuplicateID(err) && attempt < 2 {
			continue
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&pagereportv1.UploadPageResponse{
		Id:  pageID,
		Url: r.s.cfg.PageURL(pageID),
	}), nil
}

func (r *rpcService) ListPages(
	ctx context.Context,
	_ *connect.Request[pagereportv1.ListPagesRequest],
) (*connect.Response[pagereportv1.ListPagesResponse], error) {
	pages, err := r.s.store.ListPagesByOwner(ctx, OwnerFrom(ctx))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &pagereportv1.ListPagesResponse{}
	for _, p := range pages {
		resp.Pages = append(resp.Pages, r.s.pageMeta(p))
	}
	return connect.NewResponse(resp), nil
}

func (r *rpcService) GetPage(
	ctx context.Context,
	req *connect.Request[pagereportv1.GetPageRequest],
) (*connect.Response[pagereportv1.GetPageResponse], error) {
	p, err := r.s.store.GetPageForOwner(ctx, req.Msg.GetId(), OwnerFrom(ctx))
	if errors.Is(err, store.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &pagereportv1.GetPageResponse{Meta: r.s.pageMeta(p)}
	if req.Msg.GetIncludeContent() {
		resp.Content = p.Content
	}
	return connect.NewResponse(resp), nil
}

func (r *rpcService) DeletePage(
	ctx context.Context,
	req *connect.Request[pagereportv1.DeletePageRequest],
) (*connect.Response[pagereportv1.DeletePageResponse], error) {
	err := r.s.store.DeletePageForOwner(ctx, req.Msg.GetId(), OwnerFrom(ctx))
	if errors.Is(err, store.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&pagereportv1.DeletePageResponse{}), nil
}

func (r *rpcService) PrunePages(
	ctx context.Context,
	req *connect.Request[pagereportv1.PrunePagesRequest],
) (*connect.Response[pagereportv1.PrunePagesResponse], error) {
	olderThan := req.Msg.GetOlderThanSeconds()
	if olderThan <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("older_than_seconds must be positive"))
	}
	cutoff := time.Now().Add(-time.Duration(olderThan) * time.Second)
	n, err := r.s.store.PrunePagesForOwner(ctx, cutoff, OwnerFrom(ctx))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&pagereportv1.PrunePagesResponse{DeletedCount: n}), nil
}

// pageMeta is shared by both services so the CLI and the dashboard describe a
// page identically.
func (s *Server) pageMeta(p store.Page) *pagereportv1.PageMeta {
	return &pagereportv1.PageMeta{
		Id:          p.ID,
		Title:       p.Title,
		ContentType: p.ContentType,
		SizeBytes:   p.SizeBytes,
		CreatedAt:   p.CreatedAt.Unix(),
		CreatedBy:   p.CreatedBy,
		Url:         s.cfg.PageURL(p.ID),
	}
}
