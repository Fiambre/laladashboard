package bashrunner

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/internal/widgets"
)

func init() {
	registry.Register(&BashRunnerWidget{})
}

type BashRunnerWidget struct{}

func (w *BashRunnerWidget) TypeID() string      { return "bash-runner" }
func (w *BashRunnerWidget) DisplayName() string { return "Bash Runner" }

func (w *BashRunnerWidget) Render(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	return w.RenderContent(ctx, inst)
}

func (w *BashRunnerWidget) RenderContent(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	command := inst.Setting("command", "")
	if command == "" {
		return bashError("Configura el comando a ejecutar")
	}

	timeoutSec := 30
	if n, err := strconv.Atoi(inst.Setting("timeout_seconds", "30")); err == nil && n > 0 {
		timeoutSec = n
	}

	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()

	output := out.String()
	if output == "" && err != nil {
		output = stderr.String()
		if output == "" {
			output = err.Error()
		}
	}

	return bashContent(output)
}

func (w *BashRunnerWidget) ConfigSchema() []widgets.ConfigField {
	return []widgets.ConfigField{
		{Key: "command", Label: "Script", Type: "textarea", Required: true, Placeholder: "df -h | grep '/$'"},
		{Key: "timeout_seconds", Label: "Timeout (seg)", Type: "number", Default: "30"},
		{Key: "poll_seconds", Label: "Actualizar cada (seg)", Type: "number", Default: "60"},
	}
}
