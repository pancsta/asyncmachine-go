import os
import importlib.util
from grafanalib.core import Dashboard

def import_from_path(path):
    spec = importlib.util.spec_from_file_location("module.name", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

# include the shared am grafana funcs
am_grafana = import_from_path(os.getcwd()+"/am_grafana.py")

interval = '5s'
panels = []

# loop over env var IDS, divided by comma
for id in os.environ['IDS'].split(','):
    # replace - with _
    id = id.replace('-', '_')

    # append to existing panels
    panels.extend(am_grafana.mach_panels(id))


dashboard = Dashboard(
    title="Machine Dashboard: " + os.environ['IDS'],
    description="AsyncMachine internals gathered by pkg/telemetry",
    refresh='5s',
    # tags=[
    #     'example'
    # ],
    timezone="browser",
    panels=panels,
).auto_panel_ids()
