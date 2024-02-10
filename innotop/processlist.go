package innotop

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lefred/innotopgo/db"
	"github.com/mum4k/termdash"
	"github.com/mum4k/termdash/align"
	"github.com/mum4k/termdash/cell"
	"github.com/mum4k/termdash/container"
	"github.com/mum4k/termdash/keyboard"
	"github.com/mum4k/termdash/linestyle"
	"github.com/mum4k/termdash/terminal/tcell"
	"github.com/mum4k/termdash/terminal/terminalapi"
	"github.com/mum4k/termdash/widgets/barchart"
	"github.com/mum4k/termdash/widgets/sparkline"
	"github.com/mum4k/termdash/widgets/text"
	"github.com/mum4k/termdash/widgets/textinput"
)

const redrawInterval = 1000 * time.Millisecond

func Processlist(mydb *sql.DB, displaytype string) error {

	if displaytype == "simple" {
		cols, data, err := GetProcesslist(mydb)
		if err != nil {
			return err
		}
		DisplaySimple(cols, data)
	} else {
		err := DisplayProcesslist(mydb)
		if err != nil {
			ExitWithError(err)
		}
	}
	return nil
}

func GetProcesslist(mydb *sql.DB) ([]string, [][]string, error) {
	stmt := `select pps.PROCESSLIST_COMMAND AS command,
                                  pps.THREAD_ID AS thd_id, pps.PROCESSLIST_ID AS conn_id,
                                  conattr_pid.ATTR_VALUE AS pid, pps.PROCESSLIST_STATE AS state,
                                  if((pps.NAME in ('thread/sql/one_connection','thread/thread_pool/tp_one_connection')),
                                   concat(pps.PROCESSLIST_USER,'@',pps.PROCESSLIST_HOST),
                                   replace(pps.NAME,'thread/','')) AS user,
                                  pps.PROCESSLIST_DB AS db, sys.format_statement(pps.PROCESSLIST_INFO) AS current_statement,
                                  if(isnull(esc.END_EVENT_ID), format_pico_time(esc.TIMER_WAIT),NULL) AS statement_latency,
                                  format_pico_time(esc.LOCK_TIME) AS lock_latency,
                                  if(isnull(esc.END_EVENT_ID),esc.TIMER_WAIT,0) AS sort_time
                            from (performance_schema.threads pps
                            left join performance_schema.events_statements_current esc
                                on (pps.THREAD_ID = esc.THREAD_ID))
							left join performance_schema.session_connect_attrs conattr_pid
        						 on((conattr_pid.PROCESSLIST_ID = pps.PROCESSLIST_ID) and (conattr_pid.ATTR_NAME = '_pid'))
                            where pps.PROCESSLIST_ID is not null
                              and pps.PROCESSLIST_COMMAND <> 'Daemon'
                            order by sort_time desc
                        `
	rows, err := db.Query(mydb, stmt)
	if err != nil {
		return nil, nil, err
	}
	cols, data, err := db.GetData(rows)
	if err != nil {
		return nil, nil, err
	}

	return cols, data, nil
}

func periodic(ctx context.Context, interval time.Duration, fn func() error) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := fn(); err != nil {
				return err
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func DisplayProcesslistContent(mydb *sql.DB, main_window *text.Text) error {
	_, data, err := GetProcesslist(mydb)
	if err != nil {
		return err
	}
	main_window.Reset()
	header := fmt.Sprintf("%-7v %-5v %-5v %-7v %-25v %-20v %-12v %10v %10v %-65v\n",
		"Cmd", "Thd", "Conn", "Pid", "State", "User", "Db", "Time", "Lock Time", "Query")
	if err := main_window.Write(header, text.WriteCellOpts(cell.Bold())); err != nil {
		return err
	}
	var color int
	for _, row := range data {
		line := fmt.Sprintf("%-7v %-5v %-5v %-7v %-25v %-20v %-12v %10v %10v %-65v\n",
			ChunkString(row[0], 7),
			ChunkString(row[1], 5),
			ChunkString(row[2], 5),
			ChunkString(row[3], 7),
			ChunkString(row[4], 25),
			ChunkString(row[5], 20),
			ChunkString(row[6], 12),
			ChunkString(row[8], 10),
			ChunkString(row[9], 10),
			row[7])
		col_value, _ := strconv.Atoi(row[10])
		switch {
		case col_value > 60_000_000_000_000:
			color = 9 // red after 1min
		case col_value > 30_000_000_000_000:
			color = 172 // orange after 30sec
		case col_value > 10_000_000_000_000:
			color = 2 // green after 10sec
		case col_value > 5_000_000_000_000:
			color = 6 // blue after 5sec
		default:
			color = 15 // white
		}
		main_window.Write(line, text.WriteCellOpts(cell.FgColor(cell.ColorNumber(color))))
	}
	return nil
}

func BackToMainView(c *container.Container, top_window *text.Text, main_window *text.Text,
	tlg *barchart.BarChart, trg *sparkline.SparkLine, current_mode string) error {
	if current_mode == "help" || current_mode == "thread_details" || current_mode ==
		"innodb" || current_mode == "memory" || current_mode == "replication" {
		c.Update("main_container", container.Clear())
		c.Update("dyn_top_container", container.Clear())
	} else {
		top_window.Reset()
	}

	c.Update("dyn_top_container", container.SplitHorizontal(container.Top(
		container.SplitVertical(
			container.Left(
				container.Border(linestyle.Light),
				container.ID("top_container"),
				container.PlaceWidget(top_window),
				container.FocusedColor(cell.ColorNumber(15)),
			),
			container.Right(
				container.SplitVertical(
					container.Left(
						container.Border(linestyle.Light),
						container.ID("top_left_graph"),
						container.FocusedColor(cell.ColorNumber(15)),
						container.PlaceWidget(tlg),
					),
					container.Right(
						container.Border(linestyle.Light),
						container.ID("top_right_graph"),
						container.FocusedColor(cell.ColorNumber(15)),
						container.PlaceWidget(trg),
					),
					container.SplitPercent(50),
				),
			),
			container.SplitPercent(60),
		),
	),
		container.Bottom(
			container.Border(linestyle.Light),
			container.ID("main_container"),
			container.PlaceWidget(main_window),
			container.BorderTitle("Processlist (ESC to quit, ? to help)"),
			container.FocusedColor(cell.ColorNumber(15)),
		), container.SplitFixed(8)))
	//c.Update("top_container", container.Clear())
	c.Update("main_container", container.Focused())
	main_window.Reset()
	main_window.Write("\n\n... please wait...", text.WriteCellOpts(cell.FgColor(cell.ColorNumber(6)), cell.Italic()))
	return nil
}

func DisplayProcesslist(mydb *sql.DB) error {

	show_processlist := true
	processlist_drawing := false
	waiting_input := false
	current_mode := "processlist"
	thread_id := "0"

	var c *container.Container
	var status map[string]string
	var old_values []int
	status = nil

	t, err := tcell.New()
	if err != nil {
		return err
	}
	defer t.Close()

	ctx, cancel := context.WithCancel(context.Background())
	innotop, err := text.New()
	if err != nil {
		cancel()
		return err
	}

	// top window for status and query display in explain
	top_window, err := text.New(text.WrapAtWords())
	if err != nil {
		cancel()
		return err
	}

	// define an error message text box
	error_msg, err := text.New()
	if err != nil {
		cancel()
		return err
	}

	// main window for processlist and explain for example
	main_window, err := text.New()
	if err != nil {
		cancel()
		return err
	}

	// graph on top left

	tlg, err := barchart.New(
		barchart.BarColors([]cell.Color{
			cell.ColorGreen,
			cell.ColorNumber(31),
			cell.ColorNumber(172),
			cell.ColorRed,
		}),
		barchart.ValueColors([]cell.Color{
			cell.ColorWhite,
			cell.ColorWhite,
			cell.ColorWhite,
			cell.ColorWhite,
		}),
		barchart.ShowValues(),
		barchart.BarWidth(4),
		barchart.Labels([]string{
			"Sel",
			"Ins",
			"Upd",
			"Del",
		}),
	)
	if err != nil {
		cancel()
		return err
	}

	// graph on top right
	trg, err := sparkline.New(
		sparkline.Color(cell.ColorBlue),
		sparkline.Label("QPS"),
	)
	if err != nil {
		cancel()
		return err
	}

	// input box at the bottom
	bottom_input, err := textinput.New(
		textinput.MaxWidthCells(4),
		textinput.Label("Thread Id: ", cell.FgColor(cell.ColorNumber(31))),
		textinput.ClearOnSubmit(),
		textinput.OnSubmit(func(thread_id_in string) error {
			// TODO: check if thread id is a number
			reNum := regexp.MustCompile(`^\d+$`)
			if !reNum.MatchString(thread_id_in) {
				error_msg.Reset()
				error_msg.Write(fmt.Sprintf("input '%s' is not a number", thread_id_in), text.WriteCellOpts(cell.FgColor(cell.ColorNumber(172)), cell.Bold()))
				c.Update("bottom_container", container.PlaceWidget(error_msg))
				show_processlist = true
				waiting_input = false
				current_mode = "processlist"
				thread_id = "0"
				return nil
			}
			thread_id = thread_id_in
			if current_mode == "explain_normal" {
				show_processlist = false
				main_window.Reset()
				top_window.Reset()
				err := DisplayExplain(ctx, mydb, c, top_window, main_window, thread_id, "NORMAL")
				if err != nil {
					error_msg.Reset()
					error_msg.Write(fmt.Sprintf("Thread_id '%s' cannot be retrieved", thread_id_in),
						text.WriteCellOpts(cell.FgColor(cell.ColorNumber(172)), cell.Bold()))
					c.Update("bottom_container", container.PlaceWidget(error_msg))
					show_processlist = true
					BackToMainView(c, top_window, main_window, tlg, trg, current_mode)
					current_mode = "processlist"
					thread_id = "0"
				}
			} else if current_mode == "locking" {
				show_processlist = false
				main_window.Reset()
				top_window.Reset()
				err := DisplayLocking(ctx, mydb, c, top_window, main_window, thread_id)
				if err != nil {
					error_msg.Reset()
					error_msg.Write(fmt.Sprintf("Thread_id '%s' cannot be retrieved", thread_id_in),
						text.WriteCellOpts(cell.FgColor(cell.ColorNumber(172)), cell.Bold()))
					c.Update("bottom_container", container.PlaceWidget(error_msg))
					show_processlist = true
					BackToMainView(c, top_window, main_window, tlg, trg, current_mode)
					current_mode = "processlist"
					thread_id = "0"
				}
			} else if current_mode == "kill" {
				err = KillQuery(mydb, thread_id)
				if err != nil {
					error_msg.Reset()
					error_msg.Write(fmt.Sprintf("Thread_id '%s' cannot be retrieved", thread_id_in),
						text.WriteCellOpts(cell.FgColor(cell.ColorNumber(172)), cell.Bold()))
					c.Update("bottom_container", container.PlaceWidget(error_msg))
				} else {
					c.Update("bottom_container", container.Clear())
				}
				c.Update("main_container", container.Focused())
				show_processlist = true
				current_mode = "processlist"
				thread_id = "0"
			} else if current_mode == "thread_details" {
				main_window.Reset()
				top_window.Reset()
				err = DisplayThreadDetails(mydb, c, thread_id)
				if err != nil {
					error_msg.Reset()
					error_msg.Write(fmt.Sprintf("Thread_id '%s' cannot be retrieved", thread_id_in),
						text.WriteCellOpts(cell.FgColor(cell.ColorNumber(172)), cell.Bold()))
					c.Update("bottom_container", container.PlaceWidget(error_msg))
					show_processlist = true
					BackToMainView(c, top_window, main_window, tlg, trg, current_mode)
					current_mode = "processlist"
					thread_id = "0"
				}
			}
			waiting_input = false
			return nil
		}),
	)
	if err != nil {
		cancel()
		return err
	}

	_, data, err := db.GetServerInfo(mydb)
	if err != nil {
		cancel()
		return err
	}
	innotop.Write("Inno", text.WriteCellOpts(cell.BgColor(cell.ColorNumber(7)), cell.FgColor(cell.ColorNumber(31)), cell.Bold()))
	innotop.Write("Top", text.WriteCellOpts(cell.BgColor(cell.ColorNumber(7)), cell.FgColor(cell.ColorNumber(172)), cell.Bold()))
	innotop.Write(" Go ", text.WriteCellOpts(cell.BgColor(cell.ColorNumber(7)), cell.FgColor(cell.ColorNumber(31)), cell.Bold()))
	innotop.Write(version(), text.WriteCellOpts(cell.BgColor(cell.ColorNumber(7)), cell.FgColor(cell.ColorNumber(172)), cell.Bold()))
	innotop.Write(" | ", text.WriteCellOpts(cell.BgColor(cell.ColorNumber(7)), cell.FgColor(cell.ColorNumber(31)), cell.Bold()))

	var mysql_version string
	var mysql_brand string
	for _, row := range data {
		line := fmt.Sprintf("%s %s ", row[0], row[1])
		mysql_brand = row[0]
		mysql_version = row[1]
		innotop.Write(line, text.WriteCellOpts(cell.BgColor(cell.ColorNumber(7)), cell.FgColor(cell.ColorNumber(172)), cell.Italic()))
		line = fmt.Sprintf("[%s:%s]", row[2], row[3])
		innotop.Write(line, text.WriteCellOpts(cell.BgColor(cell.ColorNumber(7)), cell.FgColor(cell.ColorNumber(31)), cell.Italic()))
	}
	innotop.Write(strings.Repeat(" ", 200), text.WriteCellOpts(cell.BgColor(cell.ColorNumber(7))))
	if !strings.HasPrefix(mysql_version, "8") {
		cancel()
		fmt.Printf("\n\n... Sorry %v %v is not supported ...", mysql_brand, mysql_version)
		time.Sleep(3 * time.Second)
		return nil
	}
	main_window.Write("\n\n... please wait...", text.WriteCellOpts(cell.FgColor(cell.ColorNumber(6)), cell.Italic()))
	go periodic(ctx, 1*time.Second, func() error {
		if show_processlist {
			//top_window.Reset()
			status, old_values, err = DisplayStatus(mydb, top_window, tlg, trg, status, old_values)
			if err != nil {
				cancel()
				t.Close()
				ExitWithError(err)
			}
			if !processlist_drawing {
				processlist_drawing = true
				err = DisplayProcesslistContent(mydb, main_window)
				if err != nil {
					cancel()
					t.Close()
					ExitWithError(err)
				}
				processlist_drawing = false
			}

		}
		return nil
	})

	c, err = container.New(
		t,
		container.SplitHorizontal(
			container.Top(
				container.PlaceWidget(innotop),
			),
			container.Bottom(
				container.SplitHorizontal(
					container.Top(
						container.ID("dyn_top_container"),
						container.SplitHorizontal(
							container.Top(
								container.SplitVertical(
									container.Left(
										container.Border(linestyle.Light),
										container.ID("top_container"),
										container.PlaceWidget(top_window),
										container.FocusedColor(cell.ColorNumber(15)),
									),
									container.Right(
										container.SplitVertical(
											container.Left(
												container.Border(linestyle.Light),
												container.ID("top_left_graph"),
												container.FocusedColor(cell.ColorNumber(15)),
												container.PlaceWidget(tlg),
											),
											container.Right(
												container.Border(linestyle.Light),
												container.ID("top_right_graph"),
												container.FocusedColor(cell.ColorNumber(15)),
												container.PlaceWidget(trg),
											),
											container.SplitPercent(50),
										),
									),
									container.SplitPercent(60),
								),
							),
							container.Bottom(
								container.Border(linestyle.Light),
								container.ID("main_container"),
								container.BorderTitle("Processlist (ESC to quit, ? to help)"),
								container.PlaceWidget(main_window),
								container.FocusedColor(cell.ColorNumber(15)),
							),
							container.SplitFixed(8),
						),
					),
					container.Bottom(
						container.ID("bottom_container"),
						container.AlignHorizontal(align.HorizontalLeft),
						container.Clear(),
					),
					container.SplitPercent(99),
				),
			),
			container.SplitFixed(1),
		),
		container.ID("general_container"),
	)
	if err != nil {
		cancel()
		t.Close()
		return err
	}

	quitter := func(k *terminalapi.Keyboard) {
		if k.Key == keyboard.KeyEsc || k.Key == keyboard.KeyCtrlC {
			cancel()
		} else if k.Key == '?' {
			show_processlist = false
			current_mode = "help"
			DisplayHelp(c)
		} else if k.Key == 'm' || k.Key == 'M' {
			show_processlist = false
			current_mode = "memory"
			k2, err := DisplayMemory(mydb, c, t)
			if err != nil {
				cancel()
				t.Close()
				ExitWithError(err)
			}
			if k2 == keyboard.KeyEsc {
				cancel()
			}
			show_processlist = true
			BackToMainView(c, top_window, main_window, tlg, trg, current_mode)
			current_mode = "processlist"
			thread_id = "0"
			} else if k.Key == 'r' || k.Key == 'R' {
				show_processlist = false
				current_mode = "replication"
				k2, err := DisplayReplication(mydb, c, t)
				if err != nil {
					cancel()
					t.Close()
					ExitWithError(err)
				}
				if k2 == keyboard.KeyEsc {
					cancel()
				}
				show_processlist = true
				BackToMainView(c, top_window, main_window, tlg, trg, current_mode)
				current_mode = "processlist"
				thread_id = "0"
		} else if k.Key == 'i' || k.Key == 'I' {
			show_processlist = false
			current_mode = "innodb"
			k2, err := DisplayInnoDB(mydb, c, t)
			if err != nil {
				cancel()
				t.Close()
				ExitWithError(err)
			}
			if k2 == keyboard.KeyEsc {
				cancel()
			}
			show_processlist = true
			BackToMainView(c, top_window, main_window, tlg, trg, current_mode)
			current_mode = "processlist"
			thread_id = "0"
		} else if k.Key == 'l' || k.Key == 'L' {
			if current_mode == "processlist" {
				waiting_input = true
				c.Update("bottom_container", container.PlaceWidget(bottom_input))
				c.Update("bottom_container", container.Focused())
				current_mode = "locking"
			}
		} else if k.Key == 'e' {
			if current_mode == "processlist" {
				waiting_input = true
				c.Update("bottom_container", container.PlaceWidget(bottom_input))
				c.Update("bottom_container", container.Focused())
				current_mode = "explain_normal"
			}
		} else if k.Key == 'E' {
			show_processlist = false
			current_mode = "error_log"
			k2, err := DisplayErrorlog(mydb, c, t)
			if err != nil {
				cancel()
				t.Close()
				ExitWithError(err)
			}
			if k2 == keyboard.KeyEsc {
				cancel()
			}
			show_processlist = true
			BackToMainView(c, top_window, main_window, tlg, trg, current_mode)
			current_mode = "processlist"
			thread_id = "0"
		} else if k.Key == 'd' || k.Key == 'D' {
			if current_mode == "processlist" {
				show_processlist = false
				current_mode = "thread_details"
				waiting_input = true
				c.Update("bottom_container", container.PlaceWidget(bottom_input))
				c.Update("bottom_container", container.Focused())
			}
		} else if (k.Key == 'k' || k.Key == 'K') && show_processlist {
			waiting_input = true
			c.Update("bottom_container", container.PlaceWidget(bottom_input))
			c.Update("bottom_container", container.Focused())
			current_mode = "kill"
		} else if k.Key == keyboard.KeyBackspace2 && !waiting_input {
			if !show_processlist {
				show_processlist = true
				BackToMainView(c, top_window, main_window, tlg, trg, current_mode)
				current_mode = "processlist"
				thread_id = "0"
			}
		} else if k.Key == 'a' {
			if strings.HasPrefix(current_mode, "explain_") && current_mode != "explain_analyze" {
				main_window.Reset()
				err := DisplayExplain(ctx, mydb, c, top_window, main_window, thread_id, "ANALYZE")
				if err != nil {
					main_window.Write("Aborting... the query was too long, use <A> to ignore timeout.",
						text.WriteCellOpts(cell.FgColor(cell.ColorNumber(172)), cell.Bold()))
					current_mode = "explain_normal"
					c.Update("main_container", container.BorderTitle("EXPLAIN (<-- <Backspace> to return  -  <Space> to change EXPLAIN FORMAT)"))
				} else {
					current_mode = "explain_analyze"
				}
			}
		} else if k.Key == 'A' {
			if strings.HasPrefix(current_mode, "explain_") && current_mode != "explain_analyze" {
				main_window.Reset()
				err := DisplayExplain(ctx, mydb, c, top_window, main_window, thread_id, "ANALYZE /*NO_TIMEOUT*/ ")
				if err != nil {
					cancel()
					t.Close()
					ExitWithError(err)

				}
				current_mode = "explain_analyze"
			}
		} else if k.Key == keyboard.KeySpace {
			if current_mode == "explain_normal" {
				main_window.Reset()
				err := DisplayExplain(ctx, mydb, c, top_window, main_window, thread_id, "FORMAT=TREE")
				if err != nil {
					cancel()
					t.Close()
					ExitWithError(err)
				}
				current_mode = "explain_tree"
			} else if current_mode == "explain_tree" {
				main_window.Reset()
				err := DisplayExplain(ctx, mydb, c, top_window, main_window, thread_id, "FORMAT=JSON")
				if err != nil {
					cancel()
					t.Close()
					ExitWithError(err)
				}
				current_mode = "explain_json"
			} else if current_mode == "explain_json" {
				main_window.Reset()
				err := DisplayExplain(ctx, mydb, c, top_window, main_window, thread_id, "NORMAL")
				if err != nil {
					cancel()
					t.Close()
					ExitWithError(err)
				}
				current_mode = "explain_normal"
			} else if show_processlist {
				if !processlist_drawing {
					processlist_drawing = true
					err = DisplayProcesslistContent(mydb, main_window)
					if err != nil {
						cancel()
						t.Close()
						ExitWithError(err)
					}
					processlist_drawing = false
				}
			}
		}

	}

	if err := termdash.Run(ctx, t, c, termdash.KeyboardSubscriber(quitter), termdash.RedrawInterval(redrawInterval)); err != nil {
		return err
	}
	return nil
}

func ExitWithError(err error) {
	fmt.Printf("%s\n", err)
	os.Exit(1)
}
