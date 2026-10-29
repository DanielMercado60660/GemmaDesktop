const {solve}=require("./logic.cjs");console.log(JSON.stringify(JSON.parse(process.argv[2]).map(solve)));
