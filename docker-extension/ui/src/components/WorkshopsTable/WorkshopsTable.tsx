import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableContainer from "@mui/material/TableContainer";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import Typography from "@mui/material/Typography";
import Paper from "@mui/material/Paper";
import OpenInNewIcon from "@mui/icons-material/OpenInNew";
import DeleteIcon from "@mui/icons-material/Delete";
import DirectionsRunIcon from "@mui/icons-material/DirectionsRun";
import { Statuses, Workshop } from "../../common/types";
import { handleGoTo } from "../../common/goto";
import { Tooltip } from "@mui/material";

interface WorkshopsTableProps {
  rows: Workshop[];
  onStop: (name: string) => void;
  showPort: boolean;
}

export default function WorkshopsTable({ rows, onStop, showPort }: WorkshopsTableProps) {
  return (
    <>
      <TableContainer component={Paper} sx={{ maxHeight: "35vh" }}>
        <Table stickyHeader sx={{ minWidth: 650 }} size="small" aria-label="simple table">
          <TableHead>
            <TableRow>
              <TableCell align="center">Actions</TableCell>
              <TableCell align="center">Status</TableCell>
              <TableCell>Name</TableCell>
              {showPort && <TableCell align="left">Port</TableCell>}
              <TableCell align="left">Source</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.length > 0 ? (
              rows
                .sort((a, b) => (a.url < b.url ? -1 : 1))
                .map(row => (
                  <TableRow
                    key={row.name}
                    sx={{ "&:last-child td, &:last-child th": { border: 0 } }}
                    selected={
                      row.status === Statuses.Starting || row.status === Statuses.Running
                        ? false
                        : true
                    }
                  >
                    <TableCell align="center">
                      {row.status === Statuses.Running ? (
                        <Tooltip title={"Open " + row.url}>
                          <OpenInNewIcon
                            color="primary"
                            onClick={() => {
                              handleGoTo(row.url);
                            }}
                          />
                        </Tooltip>
                      ) : (
                        ""
                      )}
                      {row.status === Statuses.Running ? (
                        <Tooltip title="Stop workshop">
                          <DeleteIcon color="primary" onClick={() => onStop(row.name)} />
                        </Tooltip>
                      ) : (
                        ""
                      )}
                    </TableCell>
                    <TableCell align="center">
                      {row.status === Statuses.Starting ? (
                        <Tooltip title="Starting">
                          <DirectionsRunIcon color="success" />
                        </Tooltip>
                      ) : (
                        ""
                      )}
                      {row.status === Statuses.Running ? (
                        <Tooltip title="Runnning">
                          <DirectionsRunIcon color="success" />
                        </Tooltip>
                      ) : (
                        ""
                      )}
                      {row.status === Statuses.Stopping ? (
                        <Tooltip title="Stopping">
                          <DirectionsRunIcon color="error" />
                        </Tooltip>
                      ) : (
                        ""
                      )}
                    </TableCell>
                    <TableCell component="th" scope="row">
                      {row.name}
                    </TableCell>
                    {showPort && (
                      <TableCell align="left">
                        <Typography variant="body1">
                          {(() => {
                            const segments = row.url.split(":");
                            return segments.length > 2 && segments[2] ? segments[2] : "";
                          })()}
                        </Typography>
                      </TableCell>
                    )}
                    <TableCell align="left">{row.source}</TableCell>
                  </TableRow>
                ))
            ) : (
              <TableRow>
                <TableCell colSpan={5} align="center">
                  <Typography>No workshops</Typography>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </TableContainer>
    </>
  );
}
