import React, { useEffect, useState } from "react";
import { createDockerDesktopClient } from "@docker/extension-api-client";
import { Box, Grid, Link, TextField, Typography } from "@mui/material";
import BottomIntroPane from "../components/BottomIntroPane/BottomIntroPane";
import WorkshopsTable from "../components/WorkshopsTable/WorkshopsTable";
import { handleGoTo } from "../common/goto";
import { Workshop, ListResponse, DeployResponse, DeleteResponse } from "../common/types";
import { isValidURL } from "../common/validations";
import OptionsPane from "../components/OptionsPane/OptionsPane";
import { LoadingButton } from "@mui/lab";

const sampleWorkshopURL =
  "https://github.com/educates/lab-container-basics/releases/latest/download/workshop.yaml";
const workshopUrlPrefix = "http://workshop.127-0-0-1.nip.io:";

// Note: This line relies on Docker Desktop's presence as a host application.
// If you're running this React app in a browser, it won't work properly.
const client = createDockerDesktopClient();

function useDockerDesktopClient() {
  return client;
}

const firstPossiblePort = 10081;
const lastPossiblePort = 10181;

function firstAvailablePort(workshops: Workshop[]): string {
  for (var i = firstPossiblePort; i < lastPossiblePort; i++) {
    if (workshops.filter(workshop => workshop.url === workshopUrlPrefix + i).length == 0) {
      return "" + i;
    }
  }
  return "" + lastPossiblePort;
}

export function App() {
  const [url, setUrl] = useState<string>("");
  const [port, setPort] = useState<string>("" + firstPossiblePort);
  const [queryingBackend, setQueryingBackend] = useState<boolean>(false);
  const [isUrlError, setIsUrlError] = useState<boolean>(false);
  const ddClient = useDockerDesktopClient();
  const [workshops, setWorkshops] = useState<Workshop[]>([]);
  const [showPort, setShowPort] = React.useState<boolean>(
    window.localStorage.getItem("showPort")?.toLowerCase() === "true"
  );

  useEffect(() => {
    list();
  }, []);

  useEffect(() => {
    setIsUrlError(false);
  }, [url]);

  useEffect(() => {
    setPort(firstAvailablePort(workshops));
  }, [workshops]);

  const handleShowPortChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    let checked = event.target.checked;
    setShowPort(checked);
    window.localStorage.setItem("showPort", "" + checked);
  };

  useEffect(() => {
    const interval: ReturnType<typeof setInterval> = setInterval(() => {
      list();
    }, 3000);
    return () => clearInterval(interval);
  }, []);

  const list = async () => {
    console.log("list");
    ddClient.extension.vm?.service
      ?.get("/workshop/list")
      .then((result: unknown) => {
        setWorkshops(result as ListResponse);
      })
      .catch((err: unknown) => {
        console.log(err);
      });
  };

  const start = async () => {
    if (isValidURL(url)) {
      console.log("start");
      setQueryingBackend(true);
      ddClient.extension.vm?.service
        ?.get("/workshop/deploy?url=" + encodeURIComponent(url) + "&port=" + port)
        .then((result: unknown) => {
          const _ = result as DeployResponse;
          setQueryingBackend(false);
          setUrl("");
          list();
        })
        .catch((err: unknown) => {
          console.log(err);
          setQueryingBackend(false);
        });
    } else {
      setIsUrlError(true);
    }
  };

  const stop = async (name: string) => {
    console.log("stop: " + name);
    ddClient.extension.vm?.service
      ?.get("/workshop/delete?name=" + name)
      .then((result: unknown) => {
        const _ = result as DeleteResponse;
        list();
      })
      .catch((err: unknown) => {
        console.log(err);
      });
  };

  return (
    <>
      <Grid container rowSpacing={1} columnSpacing={{ xs: 2 }}>
        <Grid item xs={12} sx={{ maxHeight: "15vh" }}>
          <Typography variant="h3">Educates Training Platform</Typography>
          <Typography variant="body1" color="text.secondary">
            Run an Educates Training Platform workshop locally by providing the workshop definition
            raw URL (e.g.{" "}
            <Link
              href="#"
              onClick={() => {
                handleGoTo(sampleWorkshopURL);
              }}
            >
              example workshop
            </Link>
            {"   "}
            <Link
              href="#"
              onClick={() => {
                setUrl(sampleWorkshopURL);
              }}
            >
              (Try it out)
            </Link>
            )
          </Typography>
        </Grid>
        <Grid container alignItems="center" margin={2} sx={{ maxHeight: "10vh" }}>
          <Grid item xs={8}>
            <TextField
              error={isUrlError}
              disabled={queryingBackend}
              helperText={isUrlError ? "Url is Invalid" : ""}
              label="Workshop definition raw url"
              sx={{ width: "100%" }}
              variant="outlined"
              value={url ?? ""}
              onChange={(event: React.ChangeEvent<HTMLInputElement>) => {
                setUrl(event.target.value);
              }}
            />
          </Grid>
          <Grid item xs={1}>
            <TextField
              label="port"
              sx={{ width: "100%" }}
              variant="outlined"
              value={port ?? ""}
              onChange={(event: React.ChangeEvent<HTMLInputElement>) => {
                setPort(event.target.value);
              }}
            />
          </Grid>
          <Grid item xs={1}>
            <Box sx={{ m: 1, position: "relative" }}>
              <LoadingButton variant="contained" loading={queryingBackend} onClick={start}>
                Start
              </LoadingButton>
            </Box>
          </Grid>
          <Grid item xs={2}>
            <OptionsPane onShowPortChange={handleShowPortChange} showPort={showPort} />
          </Grid>
        </Grid>
        <Grid item xs={12}>
          <WorkshopsTable rows={workshops} onStop={stop} showPort={showPort} />
        </Grid>
      </Grid>
      <BottomIntroPane />
    </>
  );
}
