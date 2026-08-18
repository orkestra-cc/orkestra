package handlers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// TestAuthFlowLogsUseOnlyAllowlistedStructuredFields guards ordinary logs in
// credential-bearing handlers at the syntax-tree level. It catches both
// slog.String and slog.Any, aliases such as logger.Warn, and raw err.Error()
// expressions without depending on whitespace or line wrapping.
func TestAuthFlowLogsUseOnlyAllowlistedStructuredFields(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "auth_handler.go", nil, 0)
	if err != nil {
		t.Fatalf("parse auth_handler.go: %v", err)
	}
	targets := map[string]bool{
		"InitiateOAuthLogin": true, "InitiateOAuthLink": true,
		"finishOAuthLinkRedirect": true, "resolveOAuthLinkRedirect": true,
		"HandleGoogleCallbackHTTP": true, "HandleMobileGoogleAuth": true,
		"HandleMobileAppleAuth": true, "RefreshTokens": true,
		"RefreshTokensWithHeaderHTTP": true, "RefreshTokensHTTP": true,
		"GetSessionHTTP": true, "LogoutHTTP": true,
	}
	forbiddenKeys := map[string]bool{
		"error": true, "sid": true, "userUUID": true, "sessionId": true,
		"deviceId": true, "familyId": true, "ip": true,
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !targets[fn.Name.Name] || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !isOrdinaryLogCall(call) {
				return true
			}
			// JSON encoder failures operate on the fixed response schema and
			// cannot contain credentials or request identifiers.
			if logMessage(call) == "Failed to encode response" || logMessage(call) == "Failed to encode unauthenticated session response" {
				return false
			}
			ast.Inspect(call, func(child ast.Node) bool {
				nested, ok := child.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := nested.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Error" && len(nested.Args) == 0 {
					t.Errorf("%s log at %s includes raw err.Error()", fn.Name.Name, fset.Position(nested.Pos()))
				}
				if key, ok := slogAttributeKey(nested); ok && forbiddenKeys[key] {
					t.Errorf("%s log at %s uses forbidden structured key %q", fn.Name.Name, fset.Position(nested.Pos()), key)
				}
				return true
			})
			return false
		})
	}
}

func logMessage(call *ast.CallExpr) string {
	if len(call.Args) == 0 {
		return ""
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	message, _ := strconv.Unquote(lit.Value)
	return message
}

func isOrdinaryLogCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Debug", "Info", "Warn", "Error", "DebugContext", "InfoContext", "WarnContext", "ErrorContext":
		return true
	default:
		return false
	}
}

func slogAttributeKey(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 {
		return "", false
	}
	switch sel.Sel.Name {
	case "String", "Any":
	default:
		return "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	key, err := strconv.Unquote(lit.Value)
	return key, err == nil
}
