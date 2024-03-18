from grafanalib.core import (
    TimeSeries, RowPanel, Histogram,
    Target, GridPos,
)

w = 12
h = 8
interval = '5s'

x = 0
y = 0
nextCol = 0
row = 0


def next_grid_pos():
    global x, y, w, h, nextCol

    pos = GridPos(x=x, y=y, w=w, h=h)
    if nextCol == 0:
        nextCol = 1
        x = w
    else:
        nextCol = 0
        y += h
        x = 0

    return pos


def next_row_pos():
    global x, y, w, h, nextCol

    if nextCol == 1:
        x = 0
        y += h + 1
        nextCol = 0

    pos = GridPos(x=0, y=y, w=w * 2, h=1)
    y += 1

    return pos


def mach_panels(id):
    # replace - with _
    id = id.replace('-', '_')

    return [
        RowPanel(title="Machine: " + id, gridPos=next_row_pos()),

        TimeSeries(
            title="Transition changes",
            gridPos=next_grid_pos(),
            dataSource='prometheus',
            interval=interval,
            targets=[

                Target(
                    legendFormat="Queue size",
                    expr='mach_' + id + '_queue_size',
                ),
                Target(
                    legendFormat="Tx ticks",
                    expr='mach_' + id + '_tx_tick',
                ),

                Target(
                    legendFormat="Number of tx steps",
                    expr='mach_' + id + '_steps_amount',
                ),
                Target(
                    legendFormat="Called handlers",
                    expr='mach_' + id + '_handlers_amount',
                ),

                Target(
                    legendFormat="States added",
                    expr='mach_' + id + '_states_added',
                ),
                Target(
                    legendFormat="States removed",
                    expr='mach_' + id + '_states_removed',
                ),

                Target(
                    legendFormat="States touched",
                    expr='mach_' + id + '_touched',
                ),
            ],
        ),

        TimeSeries(
            title="Machine states",
            gridPos=next_grid_pos(),
            dataSource='prometheus',
            interval=interval,
            targets=[

                Target(
                    legendFormat="States referenced in relations",
                    expr='mach_' + id + '_ref_states_amount',
                ),
                Target(
                    legendFormat="Relations",
                    expr='mach_' + id + '_relations_amount',
                ),
                Target(
                    legendFormat="States active",
                    expr='mach_' + id + '_states_active_amount',
                ),
                Target(
                    legendFormat="States inactive",
                    expr='mach_' + id + '_states_inactive_amount',
                ),
            ],
        ),

        TimeSeries(
            title="Transition time",
            gridPos=next_grid_pos(),
            dataSource='prometheus',
            interval=interval,
            targets=[
                Target(
                    legendFormat="Tx time (ms)",
                    expr='mach_' + id + '_tx_time / 1000',
                ),
            ],
        ),

        Histogram(
            title="Average transition time",
            gridPos=next_grid_pos(),
            dataSource='prometheus',
            interval=interval,
            targets=[
                Target(
                    legendFormat="Tx time (ms)",
                    expr='mach_' + id + '_tx_time / 1000',
                ),
            ],
        ),

        TimeSeries(
            title="Exceptions",
            gridPos=next_grid_pos(),
            dataSource='prometheus',
            interval=interval,
            targets=[
                Target(
                    legendFormat="Exceptions",
                    expr='mach_' + id + '_exceptions_count',
                ),
            ],
        ),
    ]
