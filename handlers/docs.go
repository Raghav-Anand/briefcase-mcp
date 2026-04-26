package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/raghav-anand/briefcase-internal/models"
	"github.com/raghav-anand/briefcase-internal/validation"
	"github.com/raghav-anand/briefcase-mcp/middleware"
)

// UploadDoc handles upload_doc.
// Routes to Firestore (inline) or Cloud Storage based on content size.
func UploadDoc(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}
		title, err := req.RequireString("title")
		if err != nil {
			return mcp.NewToolResultError("title is required"), nil
		}
		docType, err := req.RequireString("doc_type")
		if err != nil {
			return mcp.NewToolResultError("doc_type is required"), nil
		}
		format, err := req.RequireString("format")
		if err != nil {
			return mcp.NewToolResultError("format is required"), nil
		}
		content, err := req.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError("content is required"), nil
		}

		if err := validation.ValidateDocType(docType); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := validation.ValidateDocFormat(format); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		session, _ := svc.DB.GetActiveSession(ctx, claims.UID, pid)
		sessionID := ""
		if session != nil {
			sessionID = session.ID
		}
		_ = logToolCall(ctx, svc, claims.UID, pid, sessionID, "upload_doc", map[string]interface{}{
			"project_id": pid,
			"title":      title,
			"doc_type":   docType,
		})

		existingDocID := req.GetString("doc_id", "")

		docID, storageType, err := svc.DB.UpsertDoc(ctx, claims.UID, pid, &models.DocInput{
			ID:        existingDocID,
			Title:     title,
			DocType:   docType,
			Format:    format,
			Content:   content,
			UpdatedBy: "claude",
			SessionID: sessionID,
		}, svc.GCS)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to save doc: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{
			"doc_id":  docID,
			"storage": storageType,
			"message": "Doc saved.",
		})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// DeleteDoc handles delete_doc. Removes the doc from Firestore and GCS if applicable.
func DeleteDoc(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}
		docID, err := req.RequireString("doc_id")
		if err != nil {
			return mcp.NewToolResultError("doc_id is required"), nil
		}

		session, _ := svc.DB.GetActiveSession(ctx, claims.UID, pid)
		sessionID := ""
		if session != nil {
			sessionID = session.ID
		}
		_ = logToolCall(ctx, svc, claims.UID, pid, sessionID, "delete_doc", map[string]interface{}{
			"project_id": pid,
			"doc_id":     docID,
		})

		if err := svc.DB.DeleteDoc(ctx, claims.UID, pid, docID, svc.GCS); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to delete doc: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{"message": "Doc deleted."})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// ListDocs handles list_docs.
func ListDocs(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}

		var docTypeFilter *string
		if dt := req.GetString("doc_type", ""); dt != "" {
			if err := validation.ValidateDocType(dt); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			docTypeFilter = &dt
		}

		docs, err := svc.DB.ListDocs(ctx, claims.UID, pid, docTypeFilter)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to list docs: %v", err)), nil
		}

		type docMeta struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			DocType   string `json:"doc_type"`
			Format    string `json:"format"`
			Version   int    `json:"version"`
			UpdatedAt string `json:"updated_at"`
		}
		metas := make([]docMeta, 0, len(docs))
		for _, d := range docs {
			metas = append(metas, docMeta{
				ID:        d.ID,
				Title:     d.Title,
				DocType:   d.DocType,
				Format:    d.Format,
				Version:   d.Version,
				UpdatedAt: d.UpdatedAt.Format("2006-01-02T15:04:05Z"),
			})
		}

		out, _ := json.Marshal(map[string]interface{}{"docs": metas})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// GetDoc handles get_doc. Fetches content from GCS if stored there.
func GetDoc(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}
		docID, err := req.RequireString("doc_id")
		if err != nil {
			return mcp.NewToolResultError("doc_id is required"), nil
		}

		doc, err := svc.DB.GetDoc(ctx, claims.UID, pid, docID, svc.GCS)
		if err != nil {
			return mcp.NewToolResultError("Doc not found. Use list_docs() to see available docs."), nil
		}

		type docResponse struct {
			Title     string `json:"title"`
			DocType   string `json:"doc_type"`
			Format    string `json:"format"`
			Content   string `json:"content"`
			Version   int    `json:"version"`
			UpdatedAt string `json:"updated_at"`
		}
		out, _ := json.MarshalIndent(docResponse{
			Title:     doc.Title,
			DocType:   doc.DocType,
			Format:    doc.Format,
			Content:   doc.Content,
			Version:   doc.Version,
			UpdatedAt: doc.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		}, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}
}
