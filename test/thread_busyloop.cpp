#include <unistd.h>
#include <thread>
#include <string.h>
#include <iostream>
#include <vector>
#include <sstream>

std::string process_name;

void printAffinity()
{
      cpu_set_t mask;
      CPU_ZERO(&mask);
      int ret = pthread_getaffinity_np(pthread_self(), sizeof(cpu_set_t), &mask);
      if (ret != 0) {
              std::cout << "Error getting thread affinity" << ret << std::endl;
      }
      int cpu;
      std::stringstream output;
      output << " thread(s) running in cpu(s) ";
      for (cpu=0; cpu < CPU_SETSIZE;cpu++) {
              if (CPU_ISSET(cpu,&mask)) output << cpu << ",";
      }
      output.seekp(-1,output.cur);
      output << std::endl;
      std::cout << output.str();
      fflush(stdout);
}

void thread_func (void)
{
        printAffinity();
        while(true);
}

int read_cores(int cores[],char *arg)
{
        int i = 0;
        arg = strtok(arg,",");

        while (arg != NULL) {
                cores[i++]=atoi(arg);
                arg = strtok(NULL,",");
        }
        return i;
}

int main(int argc, char* argv[])
{
        for (int i=0; i<argc; i++) {
                std::cout << argv[i] << " ";
        }
        std::cout << std::endl;
        int opt,exclusive_cores[10],num_excl_cores=0,i,shared_cores[10],num_shared_cores=0;

        while ((opt = getopt (argc, argv, "c:s:n:")) != -1) {
                switch (opt)
                {
                case 'c': {
                        num_excl_cores = read_cores(exclusive_cores,optarg);
                        break;
                }
                case 'n': {
                        process_name = static_cast<char *>(optarg);
                        break;
                }
                case 's': {
                        num_shared_cores = read_cores(shared_cores,optarg);
                        break;
                }
                default:
                        std::cout << "Illegal option: " << char(opt) << std::endl;
                        return(1);
                }
        }
        std::vector<std::thread> threads;
        if (num_shared_cores) {
                int ret;
                cpu_set_t  mask;
                CPU_ZERO(&mask);
                for (i=0; i<num_shared_cores; i++)
                {
                        CPU_SET(shared_cores[i],&mask);
                }
                ret = pthread_setaffinity_np(pthread_self(), sizeof(cpu_set_t), &mask);
                if (ret != 0) {
                        std::cerr << "Err::pthread_setaffinity_np(): " << ret << ":" <<
                                num_shared_cores << ":" << shared_cores[0] << std::endl;
                }
        }
        if (num_excl_cores) {
                for (i=0; i<num_excl_cores; i++)
                {
                        int ret;
                        cpu_set_t  mask;
                        CPU_ZERO(&mask);
                        CPU_SET(exclusive_cores[i],&mask);
                        std::thread thr{thread_func};
                        ret = pthread_setaffinity_np(thr.native_handle(), sizeof(cpu_set_t), &mask);
                        if (ret != 0) {
                                std::cerr << "Err::pthread_setaffinity_np(): " << ret << "\n";
                        }
                        threads.push_back(std::move(thr));

                }
        } else {
                threads.push_back(std::thread{thread_func});
                std::this_thread::sleep_for (std::chrono::seconds(1));
        }
        std::this_thread::sleep_for (std::chrono::milliseconds(1));
        std::cout << "Main thread :";
        printAffinity();
        std::cout << std::endl;
        for (auto &t : threads) t.join();
}
