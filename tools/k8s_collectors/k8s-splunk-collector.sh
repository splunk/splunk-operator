#Collect arguments
helpFunction()
{
   echo ""
   echo "Usage: $0 -d <is_diag_required> -t <target_folder> -l <limit_output_to_avoid_kubectl_describe>"
   echo "\t-d Set to true if you require diag. False by default. Field is not mandatory"
   echo "\t-t Provide target folder to store data. Field is not mandatory"
   echo "\t-l Set to true if you want to limit the collection of data by avoid kubectl describe commands. False by default. Field is not mandatory"
   echo "\t-s Set to true if you want to collect secret related data. False by default. Field is not mandatory"
   echo "\t-h Displays usage of script"
   exit 1 # Exit script after printing help
}

echo $opt

while getopts "d:t:l:" opt
do
   case "$opt" in
      d) diag="$OPTARG" ;;
      t) targetfolder="$OPTARG" ;;
      l) limitandavoiddescribe="$OPTARG" ;;
      s) getsecrets="$OPTARG" ;;
      ? ) helpFunction ;; # Print helpFunction in case parameter is non-existent
   esac
done

#initializa option variables
diag="false"
limitandavoiddescribe="false"
getsecrets="false"

# Print helpFunction in case diag flag is not used properly
if [ ! -z "$diag" ] && [ $diag != "true" ]
then
   echo "Please enter valid value for -d option i.e set to true if diag is required. Use option only if diag is required. False by default.";
   helpFunction
fi

# Determine full path of where the data needs to be collected
if [ -z "$targetfolder" ]
then
   collect_folder=$PWD/tmp-$(date "+%F-%H-%M")
else 
   collect_folder=$targetfolder/tmp-$(date "+%F-%H-%M")
fi

# Print helpFunction in case avoid describe flag is not used properly
if [ ! -z "$limitandavoiddescribe" ] && [ $limitandavoiddescribe != "true" ]
then
   echo "Please enter valid value for -l option i.e set to true if you want to avoid kubectl describe commands";
   helpFunction
fi

# Print helpFunction in getsecrets flag is not used properly
if [ ! -z "$getsecrets" ] && [ $getsecrets != "true" ]
then
   echo "Please enter valid value for -s option i.e set to true if you want secret information to be collected";
   helpFunction
fi


echo "Starting to collect data with diag $diag in folder $collect_folder \n"

#Setup directory structure
echo "Setting up directories \n"
chmod 755 k8s-splunk-collector-helper.py
mkdir -p $collect_folder/
mkdir -p $collect_folder/pod_data
mkdir -p $collect_folder/k8s_data
mkdir -p $collect_folder/k8s_data/get
if [ -z "$limitandavoiddescribe" ]
then
   mkdir -p $collect_folder/k8s_data/describe
fi
mkdir -p $collect_folder/pod_data/logs
if [ $diag == "true" ]
then
   mkdir -p $collect_folder/pod_data/diags
fi
echo "Done setting up directories \n"

## Loop through pods and get logs/diags via python script

#Get all pod related logs and diags
echo "Started collecting logs and diags \n"
python k8s-splunk-collector-helper.py -d $diag -f $collect_folder
echo "Done collecting logs and diags \n"

## Get K8S resources related data via kubectl get and describe commands

#Get cluster dump
echo "Started collecting cluster info \n"
cd $collect_folder/k8s_data/
kubectl cluster-info dump > clusterinfodump.txt
echo "Done collecting cluster info \n"

#Capture kubectl get command outputs
echo "Started collecting kubectl get command outputs \n"
cd $collect_folder/k8s_data/get;
kubectl get all >> all.txt; kubectl get all -o yaml >> all.txt; 
kubectl get nodes >> nodes.txt; kubectl get nodes -o yaml >> nodes.txt;
kubectl get pods >> pods.txt; kubectl get pods -o yaml >> pods.txt;
if [ $getsecrets == "true" ]
then
  kubectl get secrets >> secrets.txt; kubectl get secrets -o yaml >> secrets.txt
fi
kubectl get cm >> configmap.txt; kubectl get cm -o yaml >> configmap.txt;
kubectl get sts >> statefulset.txt; kubectl get sts -o yaml >> statefulset.txt;
kubectl get deployments >> deployment.txt; kubectl get deployments -o yaml >> deployment.txt;
kubectl get services >> services.txt; kubectl get services -o yaml >> services.txt;
kubectl get pvc  >> pvc.txt; kubectl get pvc -o yaml >> pvc.txt;
kubectl get pv >> pv.txt; kubectl get pv -o yaml >> pv.txt;
kubectl get sc  >> storageClass.txt; kubectl get sc -o yaml >> storageClass.txt;
kubectl get serviceaccount >> serviceaccount.txt; kubectl get serviceaccount -o yaml >> serviceaccount.txt;
kubectl get crds >> crds.txt; kubectl get crds -o yaml >> crds.txt;
kubectl api-resources | grep splunk >> api-resources.txt;
kubectl get stdaln  >> standalone.txt; kubectl get stdaln -o yaml >> standalone.txt;
kubectl get idxc >> indexerclusters.txt; kubectl get idxc -o yaml >> indexerclusters.txt;
kubectl get cm-idxc >> clustermasters.txt; kubectl get cm-idxc -o yaml >> clustermasters.txt;
kubectl get shc >> searchheadclusters.txt; kubectl get shc -o yaml >> searchheadclusters.txt;
kubectl get lm >> licensemaster.txt; kubectl get lm -o yaml >> licensemaster.txt;
echo "Done collecting kubectl get command outputs \n"

# Implement kubectl describe only if -a option is not used. Avoid describe if -a option is set to true
if [ -z "$limitandavoiddescribe" ]
then
   #Capture kubectl describe command outputs
   echo "Started collecting kubectl describe command outputs \n"
   cd $collect_folder/k8s_data/describe;
   kubectl describe all > all.txt
   kubectl describe nodes > nodes.txt
   kubectl describe pods  > pods.txt
   if [ $getsecrets == "true" ]
   then
      kubectl describe secrets  > secrets.txt
   fi
   kubectl describe cm  > configmap.txt
   kubectl describe sts  > statefulset.txt
   kubectl describe deployments  > deployment.txt
   kubectl describe services  > services.txt
   kubectl describe pvc  > pvc.txt
   kubectl describe pv  > pv.txt
   kubectl describe sc > storageClass.txt
   kubectl describe serviceaccount  > serviceaccount.txt
   kubectl describe crds  > crds.txt
   kubectl describe stdaln  > standalone.txt
   kubectl describe idxc  > indexerclusters.txt
   kubectl describe cm-idxc  > clustermasters.txt
   kubectl describe shc  > searchheadclusters.txt
   kubectl describe lm  > licensemaster.txt
   echo "Done collecting kubectl describe command outputs \n"
fi

#All done
echo "All data requried collected under folder $collect_folder\n"